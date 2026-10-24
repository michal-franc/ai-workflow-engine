package lint

import (
	"fmt"

	"github.com/michal-franc/issue-viewer/internal/workflowupgrade"
)

// checkTemplate compares the template: stamp with the template bundled in
// this issue-cli, as issue-cli workflow upgrade does, so a project falling
// behind, or a features: record missing what the file has, shows without
// anyone asking.
func checkTemplate(env *Env) []Finding {
	if env.File == "" {
		return nil
	}
	plan, err := workflowupgrade.BuildPlan(env.File, env.Raw, "", false)
	if err != nil {
		return []Finding{{Where: env.where(0), Message: "comparing with the bundled template: " + err.Error()}}
	}
	var out []Finding
	if msg := plan.StaleRecord(); msg != "" {
		out = append(out, Finding{Where: env.where(env.lineOf("features:")), Message: msg, Fix: "issue-cli workflow scan"})
	}
	where := env.where(env.lineOf("template:"))
	switch plan.Status {
	case workflowupgrade.StatusBehind:
		msg := fmt.Sprintf("template: %s@%s is behind the bundled %s@%s", plan.Template, plan.Current, plan.Template, plan.Latest)
		if n := len(plan.Proposals); n > 0 {
			msg += fmt.Sprintf(" (%d features to adopt)", n)
		}
		out = append(out, Finding{Where: where, Message: msg, Fix: "issue-cli workflow upgrade"})
	case workflowupgrade.StatusUnstamped:
		out = append(out, Finding{Where: env.where(0),
			Message: "no template: stamp, so workflow upgrade can't tell which features are new",
			Fix:     "issue-cli workflow upgrade --template <name> (or set template: custom)"})
	case workflowupgrade.StatusUnknownTemplate:
		out = append(out, Finding{Where: where, Message: plan.Message, Fix: "issue-cli workflow upgrade"})
	}
	return out
}
