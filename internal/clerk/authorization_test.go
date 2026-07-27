package clerk

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestAuthorizationIsBoundOneUseAndConsumedBeforeDispatch(t *testing.T) {
	authorizer := NewAuthorizer(t.TempDir())
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	authorizer.now = func() time.Time { return now }

	request, err := authorizer.Create("gt-123", "api")
	if err != nil {
		t.Fatal(err)
	}
	token, authorized, err := authorizer.Authorize(request.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if authorized.BeadID != "gt-123" || authorized.RigName != "api" {
		t.Fatalf("authorization binding changed: %#v", authorized)
	}

	consumed, err := authorizer.Consume(request.ID, token)
	if err != nil {
		t.Fatal(err)
	}
	if consumed.State != StateDispatching {
		t.Fatalf("state = %s, want dispatching", consumed.State)
	}
	if _, err := authorizer.Consume(request.ID, token); !errors.Is(err, ErrConsumed) {
		t.Fatalf("second Consume() error = %v, want ErrConsumed", err)
	}
}

func TestAuthorizationRejectsAgentSession(t *testing.T) {
	authorizer := NewAuthorizer(t.TempDir())
	request, err := authorizer.Create("gt-123", "api")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Getenv("GT_ROLE")
	t.Cleanup(func() { _ = os.Setenv("GT_ROLE", old) })
	if err := os.Setenv("GT_ROLE", "clerk"); err != nil {
		t.Fatal(err)
	}

	if _, _, err := authorizer.Authorize(request.ID, time.Minute); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Authorize() error = %v, want ErrUnauthorized", err)
	}
}

func TestExpiredAuthorizationCannotBeConsumed(t *testing.T) {
	authorizer := NewAuthorizer(t.TempDir())
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	authorizer.now = func() time.Time { return now }
	request, err := authorizer.Create("gt-123", "api")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := authorizer.Authorize(request.ID, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if _, err := authorizer.Consume(request.ID, token); !errors.Is(err, ErrExpired) {
		t.Fatalf("Consume() error = %v, want ErrExpired", err)
	}
}

func TestSpecialistAuthorizationPreservesNativeKind(t *testing.T) {
	authorizer := NewAuthorizer(t.TempDir())
	request, err := authorizer.CreateSpecialist("surveyor", "gt-123", "api")
	if err != nil {
		t.Fatal(err)
	}
	if request.Specialist != "surveyor" {
		t.Fatalf("request specialist = %q, want surveyor", request.Specialist)
	}

	token, authorized, err := authorizer.Authorize(request.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if authorized.Specialist != "surveyor" {
		t.Fatalf("authorized specialist = %q, want surveyor", authorized.Specialist)
	}

	consumed, err := authorizer.Consume(request.ID, token)
	if err != nil {
		t.Fatal(err)
	}
	if consumed.Specialist != "surveyor" {
		t.Fatalf("consumed specialist = %q, want surveyor", consumed.Specialist)
	}
	if err := authorizer.Complete(request.ID, "Worker", nil); err != nil {
		t.Fatal(err)
	}
	completed, err := authorizer.Get(request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Polecat != "Worker" || completed.Specialist != "surveyor" {
		t.Fatalf("completed request = %#v", completed)
	}
}

func TestSpecialistRequestRejectsUnknownKind(t *testing.T) {
	authorizer := NewAuthorizer(t.TempDir())
	if _, err := authorizer.CreateSpecialist("planner", "gt-123", "api"); err == nil {
		t.Fatal("CreateSpecialist accepted a non-native specialist kind")
	}
}
