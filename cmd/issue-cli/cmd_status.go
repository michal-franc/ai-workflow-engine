package main

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var statusCommand = &Command{
	Name:      "status",
	ShortHelp: "Print the project's stand-up from the running viewer (status.md, or --json)",
	LongHelp: `Print the project's Status page as text: the stand-up the viewer serves at
/p/<project>/status.md, byte for byte. Waiting on you, what's running, local
CI, waiting for release and the lead's ## Next. With --json, the Status model
as /p/<project>/status.json serves it.

The viewer builds the page, so it must be running. Its address is viewer_url
in projects.yaml, else the running viewer's own (viewer.json), else
http://localhost:8080; no host is typed. Read-only.

Not an issue's status: for that, issue-cli show <slug>.

Flags:
  --md     the text stand-up (the default)
  --json   the Status model as JSON

Exit codes: 0 printed, 1 viewer not reachable (the URL it tried is printed),
2 usage error.

Examples:
  issue-cli status
  issue-cli status --json`,
	Run: runStatus,
}

func init() {
	registerCommand(statusCommand)
}

// viewerHTTP is the client issue-cli calls the viewer with. A var for tests.
var viewerHTTP = &http.Client{Timeout: 5 * time.Second}

func runStatus(ctx *Context, args []string) error {
	fs := newFlagSet("status", ctx)
	md := fs.Bool("md", false, "the text stand-up (the default)")
	if err := parseFlagsArgs(ctx, fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return &usageError{msg: fmt.Sprintf("status takes no slug: it prints the project's stand-up.\nAn issue's status: %s", tracker.InjectProject(ctx.Project, "issue-cli show "+fs.Arg(0)))}
	}
	if *md && ctx.JSONOutput {
		return &usageError{msg: "status: pick --md or --json, not both"}
	}
	file := "status.md"
	if ctx.JSONOutput {
		file = "status.json"
	}
	url := fmt.Sprintf("%s/p/%s/%s", viewerBaseURL(ctx.Project), ctx.Project.Slug, file)
	body, err := viewerGet(url)
	if err != nil {
		return fmt.Errorf("viewer not reachable at %s: %v\n\nStart it: issue-viewer --config %s\nRunning elsewhere? Set viewer_url in projects.yaml.", url, err, valueOr(ctx.ConfigPath, "<projects.yaml>"))
	}
	_, err = ctx.Stdout.Write(body)
	return err
}

// viewerGet fetches url from the viewer; a non-2xx answer is an error.
func viewerGet(url string) ([]byte, error) {
	resp, err := viewerHTTP.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	return body, nil
}
