package cmd

import (
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

func TestSpecialistAutonomousDirectiveRemainsReportOnly(t *testing.T) {
	ctx := RoleContext{
		Role:       RolePolecat,
		Rig:        "api",
		Polecat:    "Worker",
		Specialist: "surveyor",
	}
	hooked := &beads.Issue{ID: "api-123", Title: "Plan API change"}

	output := captureStdout(t, func() {
		outputAutonomousDirective(ctx, hooked, false)
	})

	for _, expected := range []string{"gt done --skip-verify", "Do NOT modify code"} {
		if !strings.Contains(output, expected) {
			t.Errorf("specialist directive missing %q:\n%s", expected, output)
		}
	}
	for _, forbidden := range []string{"When all work is committed", "use the merge queue"} {
		if strings.Contains(output, forbidden) {
			t.Errorf("specialist directive contains implementation instruction %q:\n%s", forbidden, output)
		}
	}
}
