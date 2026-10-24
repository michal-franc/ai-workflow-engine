package playtestruns

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Shrunk shots: at most this wide, and at most ShrinkScreens viewport
// heights tall after scaling, cut from the top (a shot card only shows the
// top anyway).
const (
	ShrinkWidth   = 720
	ShrinkScreens = 3
	shrinkQuality = 75
)

// defaultViewportHeight is used when a shot's name has no WxH.
const defaultViewportHeight = 900

var viewportRe = regexp.MustCompile(`(\d+)x(\d+)`)

// Shrink reduces the shots of runs in root's .playtest/runs whose issue has
// been done for longer than after: each shots/*.png, and each *.png beside
// results.yaml (a game's runner saves them there), becomes a JPEG in place
// (see ShrinkWidth), results.yaml points at the new names and gains a top-level
// reduced: <date>. A run already reduced is left alone. doneSince returns
// when an issue reached done (ok false: not done).
func Shrink(root string, doneSince func(issue string) (time.Time, bool), after time.Duration, now time.Time) (Report, error) {
	var rep Report
	runs, err := List(RunsDir(root))
	if err != nil {
		return rep, err
	}
	issues := map[string]bool{}
	for _, r := range runs {
		if r.Reduced != "" || r.Issue == "" {
			continue
		}
		at, ok := doneSince(r.Issue)
		if !ok || now.Sub(at) < after {
			continue
		}
		before, afterBytes, err := shrinkRun(r.Dir, now)
		if err != nil {
			return rep, err
		}
		issues[r.Issue] = true
		rep.BytesBefore += before
		rep.BytesAfter += afterBytes
	}
	rep.Issues = len(issues)
	return rep, nil
}

// shrinkRun converts one run's PNG shots and rewrites its results.yaml. It
// returns the shots' bytes before and after.
func shrinkRun(dir string, now time.Time) (int64, int64, error) {
	shots, _ := filepath.Glob(filepath.Join(dir, "shots", "*.png"))
	beside, _ := filepath.Glob(filepath.Join(dir, "*.png"))
	shots = append(shots, beside...)
	renamed := map[string]string{} // old base name → new path from dir
	var before, after int64
	for _, p := range shots {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		out := strings.TrimSuffix(p, ".png") + ".jpg"
		n, err := shrinkShot(p, out)
		if err != nil {
			// A shot that won't decode stays as it is.
			continue
		}
		os.Remove(p)
		before += info.Size()
		after += n
		rel, _ := filepath.Rel(dir, out)
		renamed[filepath.Base(p)] = filepath.ToSlash(rel)
	}
	return before, after, markReduced(filepath.Join(dir, "results.yaml"), renamed, now)
}

// shrinkShot writes src as a reduced JPEG at dst and returns its size.
func shrinkShot(src, dst string) (int64, error) {
	f, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		return 0, err
	}
	vh := defaultViewportHeight
	if m := viewportRe.FindStringSubmatch(filepath.Base(src)); m != nil {
		if h, err := strconv.Atoi(m[2]); err == nil && h > 0 {
			vh = h
		}
	}
	small := reduce(img, ShrinkWidth, vh*ShrinkScreens)
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	if err := jpeg.Encode(out, small, &jpeg.Options{Quality: shrinkQuality}); err != nil {
		out.Close()
		os.Remove(dst)
		return 0, err
	}
	if err := out.Close(); err != nil {
		return 0, err
	}
	info, err := os.Stat(dst)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// reduce scales img down to at most maxW wide (never up), then cuts it to
// at most screens × the scaled viewport height (vh), from the top.
func reduce(img image.Image, maxW, vh int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	scale := 1.0
	if w > maxW {
		scale = float64(maxW) / float64(w)
	}
	nw := max(1, int(float64(w)*scale+0.5))
	nh := max(1, int(float64(h)*scale+0.5))
	if limit := int(float64(vh)*scale + 0.5); limit > 0 && nh > limit {
		nh = limit
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy0 := b.Min.Y + int(float64(y)/scale)
		sy1 := min(b.Max.Y, max(sy0+1, b.Min.Y+int(float64(y+1)/scale)))
		for x := 0; x < nw; x++ {
			sx0 := b.Min.X + int(float64(x)/scale)
			sx1 := min(b.Max.X, max(sx0+1, b.Min.X+int(float64(x+1)/scale)))
			var r, g, bl, a, n uint32
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					cr, cg, cb, ca := img.At(sx, sy).RGBA()
					r, g, bl, a, n = r+cr, g+cg, bl+cb, a+ca, n+1
				}
			}
			dst.Set(x, y, color.RGBA64{uint16(r / n), uint16(g / n), uint16(bl / n), uint16(a / n)})
		}
	}
	return dst
}

// markReduced rewrites results.yaml through a yaml.Node, so fields this
// package doesn't know survive: every screenshot: naming a renamed shot, and
// every entry of a game's screenshots: list, points at the new name, and
// reduced: <date> is set at the top.
func markReduced(path string, renamed map[string]string, now time.Time) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	top := doc.Content[0]
	rewriteShots(top, renamed)
	date := now.Format("2006-01-02")
	set := false
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value == "reduced" {
			top.Content[i+1].Value = date
			set = true
		}
	}
	if !set {
		top.Content = append(top.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "reduced"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: date, Style: yaml.DoubleQuotedStyle})
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func rewriteShots(n *yaml.Node, renamed map[string]string) {
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Value == "screenshot" && v.Kind == yaml.ScalarNode {
				if nn, ok := renamed[filepath.Base(v.Value)]; ok {
					v.Value = nn
				}
				continue
			}
			if k.Value == "screenshots" && v.Kind == yaml.SequenceNode {
				for _, e := range v.Content {
					if nn, ok := renamed[filepath.Base(e.Value)]; ok && e.Kind == yaml.ScalarNode {
						e.Value = nn
					}
				}
				continue
			}
			rewriteShots(v, renamed)
		}
		return
	}
	for _, c := range n.Content {
		rewriteShots(c, renamed)
	}
}
