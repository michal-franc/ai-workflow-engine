package health

import (
	"context"
	"fmt"

	"github.com/michal-franc/issue-viewer/internal/lint"
)

// The lint family: issue-cli health lint's warnings over workflow.yaml and
// the board, in total (the lint score component) and per check. It reads the present files: backfill leaves it out. See
// docs/workbench-files.md, "The lint family".
func init() {
	Register("lint", lintFamily)
	Describe("lint", "the warnings and their fixes: issue-cli health lint")
}

func lintFamily(ctx context.Context, env Env) ([]Metric, error) {
	if env.Backfill {
		return nil, ErrNoPast
	}
	if env.Project == nil {
		return nil, fmt.Errorf("no project to lint")
	}
	le, err := lint.Load(env.Project, env.Now)
	if err != nil {
		return nil, err
	}
	rep, err := lint.Run(le)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, f := range rep.Findings {
		counts[f.Check]++
	}
	out := []Metric{{Name: "lint.warnings", Value: float64(rep.Warnings), Unit: UnitCount, Help: "issue-cli health lint warnings over workflow.yaml and the board (the lint score component)"}}
	for _, c := range rep.Checks {
		if n := counts[c]; n > 0 {
			out = append(out, Metric{Name: "lint.findings", Labels: map[string]string{"check": c}, Value: float64(n), Unit: UnitCount, Help: "issue-cli health lint warnings by check"})
		}
	}
	return out, nil
}
