package witness

import (
	"reflect"
	"testing"
)

func TestAssayerPostWorkerActivationRequiresCompletedMergeRequest(t *testing.T) {
	tests := []struct {
		name    string
		payload *PolecatDonePayload
		want    bool
	}{
		{name: "completed MR", payload: &PolecatDonePayload{Exit: string(ExitTypeCompleted), MRID: "api-mr-1"}, want: true},
		{name: "completed without MR", payload: &PolecatDonePayload{Exit: string(ExitTypeCompleted)}, want: false},
		{name: "escalated MR", payload: &PolecatDonePayload{Exit: string(ExitTypeEscalated), MRID: "api-mr-1"}, want: false},
		{name: "deferred MR", payload: &PolecatDonePayload{Exit: string(ExitTypeDeferred), MRID: "api-mr-1"}, want: false},
		{name: "nil", payload: nil, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldActivatePostWorkerAssayer(test.payload); got != test.want {
				t.Fatalf("shouldActivatePostWorkerAssayer() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestWitnessRunsAssayerBeforeRefineryForCompletedMergeRequest(t *testing.T) {
	var order []string
	runAssayerBeforeRefinery(
		&PolecatDonePayload{Exit: string(ExitTypeCompleted), MRID: "api-mr-1"},
		func() { order = append(order, "assayer") },
		func() { order = append(order, "refinery") },
	)
	if !reflect.DeepEqual(order, []string{"assayer", "refinery"}) {
		t.Fatalf("Witness handoff order = %v, want Assayer before Refinery", order)
	}
}

func TestWitnessSkipsAssayerButStillNotifiesRefineryForIncompleteWork(t *testing.T) {
	var order []string
	runAssayerBeforeRefinery(
		&PolecatDonePayload{Exit: string(ExitTypeDeferred), MRID: "api-mr-1"},
		func() { order = append(order, "assayer") },
		func() { order = append(order, "refinery") },
	)
	if !reflect.DeepEqual(order, []string{"refinery"}) {
		t.Fatalf("Witness incomplete-work handoff = %v, want Refinery only", order)
	}
}
