package validations

import (
	"fmt"
	"strings"
)

// Registry maps a structured rule name to its CheckFn. Each validator file
// registers itself in init().
var Registry = map[string]CheckFn{}

func register(name string, fn CheckFn) {
	if _, dup := Registry[name]; dup {
		panic("validations: duplicate registration for " + name)
	}
	Registry[name] = fn
}

// Has reports whether the given rule name is one of the structured
// validators registered here.
func Has(rule string) bool {
	_, ok := Registry[strings.TrimSpace(rule)]
	return ok
}

// Check dispatches to the registered validator. Returns an error suitable
// for surfacing in transition output; never panics on unknown rules.
func Check(action Action, issue *IssueView, cfg Config) error {
	rule := strings.TrimSpace(action.Rule)
	fn, ok := Registry[rule]
	if !ok {
		return fmt.Errorf("unknown structured rule: %s", rule)
	}
	return fn(action, issue, cfg)
}
