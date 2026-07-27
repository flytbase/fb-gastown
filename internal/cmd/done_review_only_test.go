package cmd

import "testing"

func TestValidateReviewOnlyCompletionRejectsCodeCommits(t *testing.T) {
	if err := validateReviewOnlyCompletion(true, 1); err == nil {
		t.Fatal("review-only completion accepted a code commit")
	}
	if err := validateReviewOnlyCompletion(true, 0); err != nil {
		t.Fatalf("review-only completion rejected a clean report: %v", err)
	}
	if err := validateReviewOnlyCompletion(false, 1); err != nil {
		t.Fatalf("normal completion rejected a code commit: %v", err)
	}
}
