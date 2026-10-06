#!/usr/bin/env python3
"""Screenshot Claude artifact mockups offline with Playwright.

Usage:
  tools/mockup-shots/shots.py <dir-or-file> [--width W] [--scheme dark|light]
                              [--shot CSS=NAME ...] [--selector CSS] [--out DIR]

Takes a saved artifact: Design canvas artboards (*.dc.html) or plain HTML
pages (*.html). For a directory, every *.dc.html in it is shot (and every
*.html when there are no artboards). Writes <name>.png next to the source,
or into --out.

- The whole page is captured (full height).
- Width comes from --width, else the artboard's $preview, else 1440. When a
  box on the page scrolls sideways (a board), the viewport is widened until
  it fits, so no column is cut off.
- --shot CSS=NAME (repeatable) also shoots the first element matching CSS
  as NAME.png, e.g. --shot "#inbox-detail=inbox-question".
- --selector CSS shoots every matching element on its own as <name>-<n>.png
  (e.g. --selector "main section" for one image per panel).
- The colour scheme is pinned (--scheme, default dark) so shots don't
  depend on the machine's theme, and fonts are loaded before shooting.
- Published HTML artifacts have no <!doctype>/<head> (the skeleton is added
  at publish time); one is added before shooting so the page isn't
  rendered in quirks mode.

Design artboards need the claude.ai runtime, so they are rendered with a
small shim (dc-shim.js) that fills {{holes}}, <sc-for>, <sc-if> and
renderVals(). Rendering is static: each screen shows its initial state.

Needs Python Playwright with Chromium (`pip install playwright` and
`playwright install chromium`).
"""
import argparse
import json
import re
import sys
from pathlib import Path

from playwright.sync_api import sync_playwright

SHIM = Path(__file__).with_name("dc-shim.js")
DEFAULT_WIDTH = 1440
MAX_WIDTH = 4000


def prepare_dc(src: str) -> str:
    """Point the artboard at the offline shim and turn sc-* into <template>
    so the HTML parser keeps them in place inside tables."""
    src = src.replace('<script src="./support.js"></script>', f'<script src="{SHIM.resolve().as_uri()}"></script>')
    src = re.sub(r"<sc-for\b", '<template data-sc="for"', src)
    src = re.sub(r"<sc-if\b", '<template data-sc="if"', src)
    return re.sub(r"</sc-(for|if)>", "</template>", src)


def preview_width(src: str):
    m = re.search(r"data-props='([^']*)'", src)
    if not m:
        return None
    try:
        props = json.loads(m.group(1).replace("&#39;", "'").replace("&amp;", "&"))
    except json.JSONDecodeError:
        return None
    return props.get("$preview", {}).get("width")


def wrap_html(src: str) -> str:
    """Give a published artifact page the skeleton claude.ai adds at publish time."""
    if re.match(r"\s*<!doctype", src, re.I):
        return src
    return ('<!doctype html><html><head><meta charset="utf-8">'
            '<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">'
            "</head><body>" + src + "</body></html>")


def collect(target: Path):
    if target.is_file():
        return [target]
    files = sorted(target.glob("*.dc.html"))
    return files or sorted(target.glob("*.html"))


def overflow_px(page) -> int:
    """Widest sideways overflow of any scrolling box on the page."""
    return page.evaluate(
        """() => Math.max(0, ...Array.from(document.querySelectorAll('*')).map(el => {
             const s = getComputedStyle(el);
             return /(auto|scroll)/.test(s.overflowX) ? el.scrollWidth - el.clientWidth : 0;
           }), document.documentElement.scrollWidth - document.documentElement.clientWidth)"""
    )


def settle(page):
    page.wait_for_load_state("networkidle")
    page.evaluate("document.fonts.ready")


def shoot(page, src_file: Path, out_dir: Path, width, selector, shots):
    src = src_file.read_text()
    is_dc = src_file.name.endswith(".dc.html")
    stem = src_file.name.removesuffix(".dc.html").removesuffix(".html")
    w = width or (preview_width(src) if is_dc else None) or DEFAULT_WIDTH

    # Rendered from the source's folder so relative assets (_blob/, images) resolve.
    page_file = src_file.resolve().parent / f".shot-{stem}.html"
    page_file.write_text(prepare_dc(src) if is_dc else wrap_html(src))
    try:
        page.set_viewport_size({"width": w, "height": 900})
        page.goto(page_file.as_uri())
        settle(page)
        extra = overflow_px(page)
        if extra > 0 and not width:
            page.set_viewport_size({"width": min(w + extra + 32, MAX_WIDTH), "height": 900})
            settle(page)
        return capture(page, src_file.name, stem, out_dir, selector, shots)
    finally:
        page_file.unlink(missing_ok=True)


def unstick(page):
    """Sticky and fixed bars land in the middle of element shots; pin them in place."""
    page.evaluate(
        """() => document.querySelectorAll('*').forEach(el => {
             if (/^(sticky|fixed)$/.test(getComputedStyle(el).position)) el.style.position = 'static';
           })"""
    )


def capture(page, label, stem, out_dir, selector, shots):
    outs = []
    if shots or selector:
        unstick(page)
    for spec in shots:
        css, _, name = spec.rpartition("=")
        el = page.query_selector(css) if css else None
        if el is None:
            print(f"warning: {label}: no element matches {css!r}", file=sys.stderr)
            continue
        out = out_dir / f"{name}.png"
        el.screenshot(path=str(out))
        outs.append(out)

    if selector:
        matches = page.query_selector_all(selector)
        if not matches:
            print(f"warning: {label}: no element matches {selector!r}", file=sys.stderr)
        for i, el in enumerate(matches, 1):
            out = out_dir / f"{stem}-{i}.png"
            el.screenshot(path=str(out))
            outs.append(out)
        return outs

    out = out_dir / f"{stem}.png"
    page.screenshot(path=str(out), full_page=True)
    return outs + [out]


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("path")
    ap.add_argument("--width", type=int)
    ap.add_argument("--selector")
    ap.add_argument("--shot", action="append", default=[], metavar="CSS=NAME")
    ap.add_argument("--scheme", choices=["dark", "light"], default="dark")
    ap.add_argument("--out")
    args = ap.parse_args()

    files = collect(Path(args.path))
    if not files:
        sys.exit(f"no *.dc.html or *.html in {args.path}")
    with sync_playwright() as pw:
        browser = pw.chromium.launch()
        page = browser.new_page(device_scale_factor=1, color_scheme=args.scheme)
        for f in files:
            out_dir = Path(args.out) if args.out else f.parent
            out_dir.mkdir(parents=True, exist_ok=True)
            for out in shoot(page, f, out_dir, args.width, args.selector, args.shot):
                print(out)
        browser.close()


if __name__ == "__main__":
    main()
