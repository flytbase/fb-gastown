package assayer

import (
	"errors"
	"testing"
	"time"
)

func TestRunAuthorizationIsOneUseAndBoundToRequest(t *testing.T) {
	t.Setenv("GT_ROLE", "")
	townRoot := t.TempDir()
	if err := EnsureHome(townRoot, ""); err != nil {
		t.Fatal(err)
	}
	authorizer := NewAuthorizer(townRoot)

	request, err := authorizer.Create("gt-123", "api", "harden")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	token, _, err := authorizer.Authorize(request.ID, time.Minute)
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	consumed, err := authorizer.Consume(request.ID, token)
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if consumed.BeadID != "gt-123" || consumed.RigName != "api" || consumed.Mode != "harden" {
		t.Fatalf("unexpected consumed request: %#v", consumed)
	}
	if consumed.AuthorizedBy != "human" {
		t.Fatalf("AuthorizedBy = %q, want human", consumed.AuthorizedBy)
	}
	if _, err := authorizer.Consume(request.ID, token); err == nil {
		t.Fatal("Consume accepted a reused capability")
	}
}

func TestMayorCanAuthorizeAndConsumeRequestedRunWithoutToken(t *testing.T) {
	t.Setenv("GT_ROLE", "mayor")
	t.Setenv("BD_ACTOR", "")
	townRoot := t.TempDir()
	if err := EnsureHome(townRoot, ""); err != nil {
		t.Fatal(err)
	}
	authorizer := NewAuthorizer(townRoot)
	request, err := authorizer.Create("gt-456", "api", "harden")
	if err != nil {
		t.Fatal(err)
	}

	consumed, err := authorizer.ConsumeMayor(request.ID)
	if err != nil {
		t.Fatalf("ConsumeMayor() error = %v", err)
	}
	if consumed.State != StateConsumed || consumed.AuthorizedBy != "mayor" {
		t.Fatalf("unexpected Mayor-consumed request: %#v", consumed)
	}
	if consumed.AuthorizedAt == nil || consumed.ConsumedAt == nil || consumed.TokenHash != "" {
		t.Fatalf("Mayor authorization was not sealed correctly: %#v", consumed)
	}
}

func TestNonMayorCannotConsumeRequestedRunWithoutToken(t *testing.T) {
	t.Setenv("GT_ROLE", "assayer")
	t.Setenv("BD_ACTOR", "mayor")
	townRoot := t.TempDir()
	if err := EnsureHome(townRoot, ""); err != nil {
		t.Fatal(err)
	}
	authorizer := NewAuthorizer(townRoot)
	request, err := authorizer.Create("gt-789", "api", "harden")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := authorizer.ConsumeMayor(request.ID); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("ConsumeMayor() error = %v, want ErrUnauthorized", err)
	}
	stored, err := authorizer.Get(request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != StateRequested {
		t.Fatalf("request state = %s, want %s", stored.State, StateRequested)
	}
}

func TestOnlyNativeAssayerRoleCanClaimPostWorkerRun(t *testing.T) {
	t.Setenv("GT_ROLE", "mayor")
	t.Setenv("BD_ACTOR", "")
	if err := RequireAssayerSession(); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("RequireAssayerSession() error = %v, want ErrUnauthorized", err)
	}
	t.Setenv("GT_ROLE", "assayer")
	if err := RequireAssayerSession(); err != nil {
		t.Fatalf("RequireAssayerSession() rejected Assayer: %v", err)
	}
}

func TestDeliveryFailureSealsConsumedRequest(t *testing.T) {
	t.Setenv("GT_ROLE", "")
	townRoot := t.TempDir()
	if err := EnsureHome(townRoot, ""); err != nil {
		t.Fatal(err)
	}
	authorizer := NewAuthorizer(townRoot)
	request, err := authorizer.Create("gt-123", "api", "harden")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := authorizer.Authorize(request.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authorizer.Consume(request.ID, token); err != nil {
		t.Fatal(err)
	}
	if err := authorizer.MarkDeliveryFailed(request.ID, errors.New("session unavailable")); err != nil {
		t.Fatal(err)
	}
	sealed, err := authorizer.Get(request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.State != StateDeliveryFailed || sealed.DeliveryError != "session unavailable" {
		t.Fatalf("delivery failure was not sealed: %#v", sealed)
	}
}

func TestAgentCannotAuthorizeItsOwnRun(t *testing.T) {
	t.Setenv("GT_ROLE", "assayer")
	townRoot := t.TempDir()
	if err := EnsureHome(townRoot, ""); err != nil {
		t.Fatal(err)
	}
	authorizer := NewAuthorizer(townRoot)
	request, err := authorizer.Create("gt-123", "api", "discover")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := authorizer.Authorize(request.ID, time.Minute); err == nil {
		t.Fatal("Authorize allowed an agent role to issue capability")
	}
}

func TestAuthorizeRejectsTTLAbovePolicyCeiling(t *testing.T) {
	t.Setenv("GT_ROLE", "")
	townRoot := t.TempDir()
	if err := EnsureHome(townRoot, ""); err != nil {
		t.Fatalf("EnsureHome() error = %v", err)
	}
	authorizer := NewAuthorizer(townRoot)
	request, err := authorizer.Create("bead-1", "rig-1", "harden")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, _, err := authorizer.Authorize(request.ID, 11*time.Minute); err == nil {
		t.Fatal("Authorize allowed a TTL above the Phase 1 policy ceiling")
	}
	stored, err := authorizer.Get(request.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.State != StateRequested {
		t.Fatalf("request state = %s, want %s", stored.State, StateRequested)
	}
}
