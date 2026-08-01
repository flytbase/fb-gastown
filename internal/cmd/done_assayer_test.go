package cmd

import (
	"errors"
	"reflect"
	"testing"

	"github.com/steveyegge/gastown/internal/assayer"
)

func TestPostWorkerAssuranceRunsBeforeRefineryAndCannotBlockIt(t *testing.T) {
	var order []string
	wantErr := errors.New("Assayer unavailable")
	_, err := runPostWorkerAssuranceHandoff(
		func() (*assayer.PostWorkerDispatchResult, error) {
			order = append(order, "assayer")
			return nil, wantErr
		},
		func() { order = append(order, "refinery") },
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("handoff error = %v, want %v", err, wantErr)
	}
	if !reflect.DeepEqual(order, []string{"assayer", "refinery"}) {
		t.Fatalf("handoff order = %v, want Assayer before Refinery", order)
	}
}
