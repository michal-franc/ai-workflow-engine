package tracker

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeQuestionIssue(t *testing.T, body string) string {
	t.Helper()
	fp := filepath.Join(t.TempDir(), "gold.md")
	content := "---\ntitle: \"Gold shop\"\nstatus: \"in design\"\n---\n\n" + body + "\n"
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatalf("write issue: %v", err)
	}
	return fp
}

func readBody(t *testing.T, fp string) string {
	t.Helper()
	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	issue, err := ParseIssue(filepath.Base(fp), data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return issue.BodyRaw
}

func goldQuestion() NewQuestion {
	return NewQuestion{
		Text:    "Does unspent gold carry over between stages?",
		Context: "At PROBE_N=200 teams bank about 140 gold at a stage lock.",
		Options: []QuestionOption{
			{ID: "A", Label: "Reset to the stage's starting gold", Tradeoff: "Simplest."},
			{ID: "B", Label: "Carry over, capped at 300", Tradeoff: "Rewards saving without snowballing.", Recommended: true},
			{ID: "C", Label: "Carry over in full"},
		},
		Links: []string{"docs/economy.md"},
	}
}

var askedAt = time.Date(2026, 10, 2, 14, 5, 0, 0, time.Local)

func TestParseOptionSpec(t *testing.T) {
	o, err := ParseOptionSpec("b=Carry over, capped at 300|Rewards saving: no snowball")
	if err != nil {
		t.Fatalf("ParseOptionSpec: %v", err)
	}
	if o.ID != "B" || o.Label != "Carry over, capped at 300" || o.Tradeoff != "Rewards saving: no snowball" {
		t.Fatalf("got %+v", o)
	}
	for _, bad := range []string{"Carry over", "AB=x", "1=x", "A=", "A=|only tradeoff"} {
		if _, err := ParseOptionSpec(bad); err == nil {
			t.Errorf("ParseOptionSpec(%q) = nil error, want error", bad)
		}
	}
}

func TestValidateNewQuestion(t *testing.T) {
	q := goldQuestion()
	if err := ValidateNewQuestion(q); err != nil {
		t.Fatalf("valid question rejected: %v", err)
	}
	one := q
	one.Options = q.Options[:1]
	if err := ValidateNewQuestion(one); err == nil || !strings.Contains(err.Error(), "at least 2 options") {
		t.Fatalf("one option: err = %v", err)
	}
	dup := q
	dup.Options = []QuestionOption{{ID: "A", Label: "x"}, {ID: "A", Label: "y"}}
	if err := ValidateNewQuestion(dup); err == nil {
		t.Fatal("duplicate ids accepted")
	}
	empty := q
	empty.Text = "  "
	if err := ValidateNewQuestion(empty); err == nil {
		t.Fatal("empty text accepted")
	}
}

func TestAddQuestion_CreatesSectionAndRoundTrips(t *testing.T) {
	fp := writeQuestionIssue(t, "## Design\n- [x] Approach documented")
	id, err := AddQuestion(fp, goldQuestion(), askedAt)
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	if id != "Q-1" {
		t.Fatalf("id = %q, want Q-1", id)
	}
	body := readBody(t, fp)
	if !strings.Contains(body, "## Questions\n\n### Q-1 · open · 2026-10-02 14:05\nDoes unspent gold carry over between stages?") {
		t.Fatalf("question not rendered as expected:\n%s", body)
	}
	if !strings.Contains(body, "- **B** (recommended) Carry over, capped at 300 — Rewards saving without snowballing.") {
		t.Fatalf("recommended option missing:\n%s", body)
	}

	qs := OpenQuestions(body)
	if len(qs) != 1 {
		t.Fatalf("open questions = %d, want 1", len(qs))
	}
	q := qs[0]
	if q.ID != "Q-1" || q.Text != "Does unspent gold carry over between stages?" || !q.AskedAt.Equal(askedAt) {
		t.Fatalf("parsed question = %+v", q)
	}
	if q.Context != "At PROBE_N=200 teams bank about 140 gold at a stage lock." {
		t.Fatalf("context = %q", q.Context)
	}
	if len(q.Options) != 3 || !q.Options[1].Recommended || q.Options[2].Tradeoff != "" || q.Options[0].Tradeoff != "Simplest." {
		t.Fatalf("options = %+v", q.Options)
	}
	if len(q.Links) != 1 || q.Links[0] != "docs/economy.md" {
		t.Fatalf("links = %v", q.Links)
	}

	id2, err := AddQuestion(fp, NewQuestion{Text: "Second?", Options: []QuestionOption{{ID: "A", Label: "yes"}, {ID: "B", Label: "no"}}}, askedAt)
	if err != nil || id2 != "Q-2" {
		t.Fatalf("second AddQuestion = %q, %v", id2, err)
	}
	if got := len(OpenQuestions(readBody(t, fp))); got != 2 {
		t.Fatalf("open questions after second ask = %d, want 2", got)
	}
}

func TestAnswerQuestion_WritesDecisionAndCollapses(t *testing.T) {
	fp := writeQuestionIssue(t, "## Design\nText.\n\n## Acceptance Criteria\n- [ ] works")
	if _, err := AddQuestion(fp, goldQuestion(), askedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := AddQuestion(fp, NewQuestion{Text: "Second?", Options: []QuestionOption{{ID: "A", Label: "yes"}, {ID: "B", Label: "no"}}}, askedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := BeginQuestionWait(fp, "Q-1", askedAt); err != nil {
		t.Fatal(err)
	}

	answeredAt := time.Date(2026, 10, 2, 14, 46, 0, 0, time.Local)
	res, err := AnswerQuestion(fp, "q-1", "b", "B, but revisit\nafter the economy pass", "", answeredAt)
	if err != nil {
		t.Fatalf("AnswerQuestion: %v", err)
	}
	want := `- **D-1** · 2026-10-02 · answered in lead chat (Q-1): Does unspent gold carry over between stages? → **B. Carry over, capped at 300**, "B, but revisit after the economy pass" (also considered: A Reset to the stage's starting gold; C Carry over in full)`
	if res.DecisionLine != want {
		t.Fatalf("decision line:\n got %s\nwant %s", res.DecisionLine, want)
	}

	body := readBody(t, fp)
	if !strings.Contains(body, "## Decisions\n\n"+want) {
		t.Fatalf("decision not written:\n%s", body)
	}
	if !strings.Contains(body, "### Q-1 · answered 2026-10-02 14:46 → B · D-1\nDoes unspent gold carry over between stages?\n\n### Q-2 · open") {
		t.Fatalf("question not collapsed to pointer:\n%s", body)
	}
	if strings.Contains(body, "PROBE_N") {
		t.Fatalf("answered question kept its context:\n%s", body)
	}
	if !strings.Contains(body, "## Acceptance Criteria\n- [ ] works") {
		t.Fatalf("other sections damaged:\n%s", body)
	}

	q := FindQuestion(body, "Q-1")
	if q == nil || q.State != QuestionAnswered || q.Answer != "B" || q.Decision != "D-1" {
		t.Fatalf("answered question parsed as %+v", q)
	}
	if got := OpenQuestions(body); len(got) != 1 || got[0].ID != "Q-2" {
		t.Fatalf("open questions after answer = %+v", got)
	}

	store, _ := LoadStats(fp)
	if _, ok := store.QuestionWaits["Q-1"]; ok {
		t.Fatal("question wait not cleared on answer")
	}

	if _, err := AnswerQuestion(fp, "Q-1", "A", "", "", answeredAt); !errors.Is(err, ErrQuestionNotOpen) {
		t.Fatalf("second answer err = %v, want ErrQuestionNotOpen", err)
	}

	res2, err := AnswerQuestion(fp, "Q-2", "A", "", "", answeredAt)
	if err != nil {
		t.Fatal(err)
	}
	if res2.DecisionID != "D-2" {
		t.Fatalf("second decision id = %s, want D-2", res2.DecisionID)
	}
	body = readBody(t, fp)
	if !strings.Contains(body, want+"\n- **D-2**") {
		t.Fatalf("decisions not kept as a tight list:\n%s", body)
	}
}

func TestAnswerQuestion_UnknownOptionAndQuestion(t *testing.T) {
	fp := writeQuestionIssue(t, "Body.")
	if _, err := AddQuestion(fp, goldQuestion(), askedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := AnswerQuestion(fp, "Q-1", "Z", "", "", askedAt); err == nil || !strings.Contains(err.Error(), "no option") {
		t.Fatalf("unknown option err = %v", err)
	}
	if _, err := AnswerQuestion(fp, "Q-9", "A", "", "", askedAt); !errors.Is(err, ErrQuestionNotOpen) {
		t.Fatalf("unknown question err = %v", err)
	}
	if _, err := AnswerQuestion(fp, "Q-1", "A", "", "chat: owner", askedAt); err == nil || !strings.Contains(err.Error(), "must not contain ':'") {
		t.Fatalf("how with ':' err = %v", err)
	}
	if got := len(OpenQuestions(readBody(t, fp))); got != 1 {
		t.Fatalf("failed answer changed the file: %d open", got)
	}
	res, err := AnswerQuestion(fp, "Q-1", "A", "", "playtest", askedAt)
	if err != nil || !strings.Contains(res.DecisionLine, "· answered in playtest (Q-1): ") {
		t.Fatalf("custom how: %v %+v", err, res)
	}
}

func TestParseQuestions_IgnoresFencedExamples(t *testing.T) {
	body := "## Questions\n\n```markdown\n### Q-7 · open · 2026-10-02 14:05\nExample?\n```\n"
	if got := ParseQuestions(body); len(got) != 0 {
		t.Fatalf("fenced example parsed as question: %+v", got)
	}
}

func TestBeginQuestionWait_KeepsEarliestStart(t *testing.T) {
	fp := writeQuestionIssue(t, "Body.")
	first, err := BeginQuestionWait(fp, "Q-1", askedAt)
	if err != nil {
		t.Fatal(err)
	}
	again, err := BeginQuestionWait(fp, "Q-1", askedAt.Add(9*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !again.Equal(first) {
		t.Fatalf("re-run wait start = %v, want %v", again, first)
	}
	if err := EndQuestionWait(fp, "Q-1"); err != nil {
		t.Fatal(err)
	}
	store, _ := LoadStats(fp)
	if len(store.QuestionWaits) != 0 {
		t.Fatalf("waits after end = %v", store.QuestionWaits)
	}
}
