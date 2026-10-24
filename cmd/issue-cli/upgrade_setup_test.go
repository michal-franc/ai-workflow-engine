package main

import (
	"bytes"
	"testing"

	"github.com/michal-franc/issue-viewer/internal/workflowupgrade"
)

func TestUpgradePrintsTheSetupSteps(t *testing.T) {
	plan := &workflowupgrade.Plan{Proposals: []workflowupgrade.Proposal{{
		Number: 1, ID: "privacy", Title: "Private work files", Snippet: "privacy:",
		Setup: []workflowupgrade.SetupStep{{Who: "agent", Do: "Record the answer."}, {Who: "owner", Do: "Create a private repo."}},
	}}}
	var out bytes.Buffer
	printUpgradePlan(&out, plan)
	assertContains(t, out.String(), "  Setup (in order; an owner step is the owner's to do):\n    1. [agent] Record the answer.\n    2. [owner] Create a private repo.\n")
}
