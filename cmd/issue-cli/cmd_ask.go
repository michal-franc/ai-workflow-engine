package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/michal-franc/issue-viewer/internal/tracker"
)

var askCommand = &Command{
	Name:      "ask",
	ShortHelp: "Ask the human a structured question (answered in the viewer inbox)",
	LongHelp: `Write a structured question into the issue's ## Questions section. The human
answers it in the viewer inbox (/p/<project>/inbox). The answer is recorded as a
dated D-n line under ## Decisions, so it survives even if this session ends.

Questions do not gate transitions; approvals stay the gate.

Options are "ID=Label|Tradeoff" with a one-letter ID (A–Z); at least 2 are
required. --recommend names the option you would pick.

--wait blocks until the question is answered and prints the decision.
--wait-for Q-n blocks on a question asked earlier. --timeout exits 3 with
nothing changed; re-run the same command to keep waiting.

Examples:
  issue-cli ask <slug> --question "Does unspent gold carry over between stages?" \
    --context "At PROBE_N=200 teams bank ~140 gold at a stage lock." \
    --option "A=Reset to the stage's starting gold|Simplest" \
    --option "B=Carry over, capped at 300|Rewards saving without snowballing" \
    --recommend B --link docs/economy.md --wait --timeout 9m
  issue-cli ask <slug> --wait-for Q-3 --timeout 9m`,
	Run: runAsk,
}

func init() {
	registerCommand(askCommand)
}

type askOutput struct {
	Slug     string `json:"slug"`
	Question string `json:"question"`
	State    string `json:"state"`
	Answer   string `json:"answer,omitempty"`
	Decision string `json:"decision,omitempty"`
	Ref      string `json:"decision_ref,omitempty"`
	Line     string `json:"decision_line,omitempty"`
	Inbox    string `json:"inbox"`
	Waited   int64  `json:"waited_seconds,omitempty"`
}

func runAsk(ctx *Context, args []string) error {
	slug, rest, err := requireSlug(args, "ask")
	if err != nil {
		return err
	}
	fs := newFlagSet("ask", ctx)
	questionFlag := fs.String("question", "", "the question, one line")
	contextFlag := fs.String("context", "", "why it matters: evidence, numbers, what each answer changes")
	var options []tracker.QuestionOption
	fs.Func("option", `repeatable: "ID=Label|Tradeoff" (ID is one letter A–Z)`, func(v string) error {
		o, err := tracker.ParseOptionSpec(v)
		if err != nil {
			return err
		}
		options = append(options, o)
		return nil
	})
	recommendFlag := fs.String("recommend", "", "option ID you recommend")
	var links []string
	fs.Func("link", "repeatable: doc, diff or file the human should read", func(v string) error {
		if v = strings.TrimSpace(v); v != "" {
			links = append(links, v)
		}
		return nil
	})
	waitForFlag := fs.String("wait-for", "", "block until an existing question (Q-n) is answered")
	opts := waitOptions{
		wait:     fs.Bool("wait", false, "after asking, block until the question is answered"),
		timeout:  fs.Duration("timeout", 0, "with --wait/--wait-for: give up after this long and exit 3 (0 = wait forever)"),
		interval: fs.Duration("interval", 2*time.Second, "with --wait/--wait-for: how often to re-check the issue"),
	}
	if err := parseFlags(ctx, fs, rest); err != nil {
		return err
	}

	waitFor := strings.ToUpper(strings.TrimSpace(*waitForFlag))
	if waitFor != "" {
		*opts.wait = true
		if *questionFlag != "" || len(options) > 0 {
			return fmt.Errorf("--wait-for waits on an existing question; drop --question/--option, or drop --wait-for to ask a new one")
		}
	}
	if err := opts.validate(); err != nil {
		return fmt.Errorf("%s (--timeout applies to --wait or --wait-for)", err)
	}

	issue, _, err := findIssueOrErr(ctx, slug)
	if err != nil {
		return err
	}
	inbox := inboxURL(ctx.Project, issue.Slug)

	id := waitFor
	if id == "" {
		q := tracker.NewQuestion{Text: *questionFlag, Context: normalizeEscapedText(*contextFlag), Options: options, Links: links}
		if rec := strings.ToUpper(strings.TrimSpace(*recommendFlag)); rec != "" {
			found := false
			for i := range q.Options {
				if q.Options[i].ID == rec {
					q.Options[i].Recommended = true
					found = true
				}
			}
			if !found {
				return fmt.Errorf("--recommend %s does not name an option (have %s)", rec, optionIDs(options))
			}
		}
		if err := tracker.ValidateNewQuestion(q); err != nil {
			return fmt.Errorf("%w\n\nExample:\n  issue-cli ask %s --question \"...?\" --option \"A=First|tradeoff\" --option \"B=Second|tradeoff\" --recommend A", err, slug)
		}
		id, err = tracker.AddQuestion(issue.FilePath, q, ctx.now())
		if err != nil {
			return fmt.Errorf("failed to add question: %w", err)
		}
		if !*opts.wait {
			out := askOutput{Slug: issue.Slug, Question: id, State: tracker.QuestionOpen, Inbox: inbox}
			if ctx.JSONOutput {
				return writeJSON(ctx.Stdout, out)
			}
			fmt.Fprintf(ctx.Stdout, "✓ Asked %s on %s\n", id, issue.Slug)
			fmt.Fprintf(ctx.Stdout, "file: %s\n", issue.FilePath)
			fmt.Fprintf(ctx.Stdout, "Answer: %s\n", inbox)
			fmt.Fprintf(ctx.Stdout, "Next: issue-cli ask %s --wait-for %s --timeout %s   (blocks until answered; exit 3 = still waiting, re-run it)\n", issue.Slug, id, suggestedWaitTimeout)
			return nil
		}
		if !ctx.JSONOutput {
			fmt.Fprintf(ctx.Stdout, "✓ Asked %s on %s\n", id, issue.Slug)
		}
	}

	return waitForAnswer(ctx, issue, id, inbox, opts)
}

// waitForAnswer blocks until question id is answered, polling the issue file
// (the mailbox) — nothing depends on the viewer reaching this process.
func waitForAnswer(ctx *Context, issue *tracker.Issue, id, inbox string, opts waitOptions) error {
	body, err := readIssueBody(issue.FilePath)
	if err != nil {
		return err
	}
	q := tracker.FindQuestion(body, id)
	if q == nil {
		return fmt.Errorf("%s has no question %s\n\nOpen questions: %s", issue.Slug, id, questionIDs(tracker.OpenQuestions(body)))
	}
	started := ctx.now()
	if q.State == tracker.QuestionOpen {
		if started, err = tracker.BeginQuestionWait(issue.FilePath, q.ID, ctx.now()); err != nil {
			return err
		}
		fmt.Fprintf(ctx.Stderr, "Waiting for an answer to %s — answer: %s (timeout: %s)\n", q.ID, inbox, opts.timeoutLabel())
	}

	var answered *tracker.Question
	err = pollUntil(ctx, opts, func() (bool, error) {
		body, err := readIssueBody(issue.FilePath)
		if err != nil {
			return false, err
		}
		cur := tracker.FindQuestion(body, q.ID)
		if cur == nil {
			return false, fmt.Errorf("question %s disappeared from %s", q.ID, issue.Slug)
		}
		if cur.State == tracker.QuestionAnswered {
			answered = cur
			return true, nil
		}
		return false, nil
	})
	if errors.Is(err, errWaitTimedOut) {
		return &exitCodeError{
			Code: exitWaitTimeout,
			Msg: fmt.Sprintf("Still waiting for an answer to %s after %s — nothing changed.\nRe-run to keep waiting (the wait's start time is kept):\n  issue-cli ask %s --wait-for %s --timeout %s",
				q.ID, opts.timeout, issue.Slug, q.ID, suggestedWaitTimeout),
		}
	}
	if err != nil {
		return err
	}

	body, _ = readIssueBody(issue.FilePath)
	line := decisionLine(body, answered.Decision)
	waited := ctx.now().Sub(started).Round(time.Second)
	if ctx.JSONOutput {
		return writeJSON(ctx.Stdout, askOutput{
			Slug: issue.Slug, Question: answered.ID, State: answered.State, Answer: answered.Answer,
			Decision: answered.Decision, Ref: issue.Slug + "#" + answered.Decision, Line: line, Inbox: inbox, Waited: int64(waited / time.Second),
		})
	}
	fmt.Fprintf(ctx.Stdout, "✓ %s answered: %s (recorded as %s#%s under ## Decisions; cite it as that ref)\n", answered.ID, answered.Answer, issue.Slug, answered.Decision)
	if line != "" {
		fmt.Fprintln(ctx.Stdout, line)
	}
	if waited > 0 {
		fmt.Fprintf(ctx.Stdout, "✓ Waited: %s\n", waited)
	}
	return nil
}

func readIssueBody(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	body, err := tracker.ParseFrontmatter(string(data), &tracker.Issue{})
	if err != nil {
		return "", err
	}
	body, _ = tracker.ParseComments(body)
	return body, nil
}

// decisionLine returns the `- **D-n** …` line for id from ## Decisions.
func decisionLine(body, id string) string {
	if id == "" {
		return ""
	}
	prefix := "- **" + id + "**"
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func optionIDs(options []tracker.QuestionOption) string {
	ids := make([]string, 0, len(options))
	for _, o := range options {
		ids = append(ids, o.ID)
	}
	if len(ids) == 0 {
		return "none"
	}
	return strings.Join(ids, ", ")
}

func questionIDs(qs []tracker.Question) string {
	ids := make([]string, 0, len(qs))
	for _, q := range qs {
		ids = append(ids, q.ID)
	}
	if len(ids) == 0 {
		return "none"
	}
	return strings.Join(ids, ", ")
}

// inboxURL links the viewer inbox with the issue's questions selected.
func inboxURL(proj *tracker.Project, slug string) string {
	base := strings.TrimRight(viewerBaseURL(), "/")
	if proj != nil {
		return fmt.Sprintf("%s/p/%s/inbox?issue=%s", base, proj.Slug, slug)
	}
	return fmt.Sprintf("%s/inbox?issue=%s", base, slug)
}

// printOpenQuestions renders the open-questions block shared by show and
// checklist, so a re-dispatched agent sees what is still unanswered.
func printOpenQuestions(ctx *Context, issue *tracker.Issue) {
	open := tracker.OpenQuestions(issue.BodyRaw)
	if len(open) == 0 {
		return
	}
	fmt.Fprintf(ctx.Stdout, "== Open questions (%d) ==\n", len(open))
	for _, q := range open {
		fmt.Fprintf(ctx.Stdout, "  %s  %s\n", q.ID, q.Text)
	}
	fmt.Fprintf(ctx.Stdout, "Answered in the inbox: %s\n", inboxURL(ctx.Project, issue.Slug))
	fmt.Fprintf(ctx.Stdout, "Block on one: issue-cli ask %s --wait-for %s --timeout %s\n\n", issue.Slug, open[0].ID, suggestedWaitTimeout)
}
