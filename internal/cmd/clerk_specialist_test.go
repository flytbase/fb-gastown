package cmd

import (
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/specialist"
)

func TestClerkNativeSpecialistCommandsAreRegistered(t *testing.T) {
	uses := make(map[string]bool)
	for _, command := range clerkCmd.Commands() {
		uses[strings.Fields(command.Use)[0]] = true
	}
	for _, name := range []string{"summon", "specialists"} {
		if !uses[name] {
			t.Fatalf("gt clerk %s is not registered", name)
		}
	}
}

func TestApplySpecialistDispatchUsesNativeReportOnlyLifecycle(t *testing.T) {
	definition, err := specialist.Resolve("scrutineer")
	if err != nil {
		t.Fatal(err)
	}
	params := SlingParams{FormulaName: "mol-polecat-work"}

	applySpecialistDispatch(&params, definition)

	if params.Specialist != "scrutineer" || !params.NoMerge || !params.ReviewOnly ||
		!params.HookRawBead || params.FormulaName != "" {
		t.Fatalf("specialist SlingParams = %#v", params)
	}
}
