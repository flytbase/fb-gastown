package cmd

import "testing"

func TestAssayerBashAllowlist(t *testing.T) {
	requestID := "0123456789abcdef0123456789abcdef"
	tests := []struct {
		command string
		want    bool
	}{
		{"gt prime", true},
		{"gt assayer status", true},
		{"gt assayer workers", true},
		{"gt assayer inspect " + requestID, true},
		{"gt assayer claim " + requestID, true},
		{"gt assayer complete " + requestID + " --outcome advisory_complete", true},
		{"gt assayer complete " + requestID + " --outcome blocked", true},
		{"gt assayer authorize " + requestID, false},
		{"git status", false},
		{"go test ./...", false},
		{"gt assayer status; git status", false},
	}
	for _, test := range tests {
		if got := assayerBashAllowed(test.command); got != test.want {
			t.Errorf("assayerBashAllowed(%q) = %t, want %t", test.command, got, test.want)
		}
	}
}

func TestAssayerCannotUseRawSlingDispatch(t *testing.T) {
	t.Setenv("GT_ROLE", "assayer")
	t.Setenv("BD_ACTOR", "")
	if !assayerDispatchDenied() {
		t.Fatal("Assayer identity did not deny raw sling dispatch")
	}
}
