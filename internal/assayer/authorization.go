package assayer

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/atomicfile"
	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/lock"
)

type RequestState string

type RequestOrigin string

const (
	StateRequested      RequestState = "requested"
	StateAuthorized     RequestState = "authorized"
	StateConsumed       RequestState = "consumed"
	StateDeliveryFailed RequestState = "delivery_failed"
)

const (
	OriginManual     RequestOrigin = "manual"
	OriginPostWorker RequestOrigin = "post_worker"
)

var (
	ErrUnauthorized = errors.New("Assayer run is not authorized")
	ErrExpired      = errors.New("Assayer run authorization expired")
	ErrConsumed     = errors.New("Assayer run authorization was already consumed")
)

// Request binds one Phase 1 run to a task, rig, and explicit operating mode.
type Request struct {
	ID            string        `json:"id"`
	BeadID        string        `json:"bead_id"`
	RigName       string        `json:"rig_name"`
	Mode          string        `json:"mode"`
	Origin        RequestOrigin `json:"origin,omitempty"`
	PolecatName   string        `json:"polecat_name,omitempty"`
	Branch        string        `json:"branch,omitempty"`
	MRID          string        `json:"mr_id,omitempty"`
	State         RequestState  `json:"state"`
	TokenHash     string        `json:"token_hash,omitempty"`
	RequestedAt   time.Time     `json:"requested_at"`
	AuthorizedAt  *time.Time    `json:"authorized_at,omitempty"`
	AuthorizedBy  string        `json:"authorized_by,omitempty"`
	ExpiresAt     *time.Time    `json:"expires_at,omitempty"`
	ConsumedAt    *time.Time    `json:"consumed_at,omitempty"`
	DeliveryError string        `json:"delivery_error,omitempty"`
}

// Authorizer owns one-use human capabilities for Assayer runs.
type Authorizer struct {
	townRoot string
	now      func() time.Time
}

func NewAuthorizer(townRoot string) *Authorizer {
	return &Authorizer{townRoot: townRoot, now: time.Now}
}

func (a *Authorizer) requestsDir() string {
	return filepath.Join(a.townRoot, ".runtime", "assayer", "requests")
}

func (a *Authorizer) ensureDir() error {
	return os.MkdirAll(a.requestsDir(), 0o700)
}

func (a *Authorizer) requestPath(id string) (string, error) {
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return "", fmt.Errorf("invalid Assayer request id %q", id)
	}
	return filepath.Join(a.requestsDir(), id+".json"), nil
}

// Create creates an unprivileged request. It does not start a run.
func (a *Authorizer) Create(beadID, rigName, mode string) (*Request, error) {
	beadID, rigName, mode = strings.TrimSpace(beadID), strings.TrimSpace(rigName), strings.ToLower(strings.TrimSpace(mode))
	if beadID == "" || rigName == "" {
		return nil, fmt.Errorf("bead id and rig name are required")
	}
	policy, err := LoadPolicy(a.townRoot)
	if err != nil {
		return nil, err
	}
	if !contains(policy.Modes, mode) {
		return nil, fmt.Errorf("unsupported Assayer mode %q", mode)
	}
	if err := a.ensureDir(); err != nil {
		return nil, err
	}
	id, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	request := &Request{ID: id, BeadID: beadID, RigName: rigName, Mode: mode, Origin: OriginManual, State: StateRequested, RequestedAt: a.now().UTC()}
	path, _ := a.requestPath(id)
	if err := atomicfile.WriteJSONWithPerm(path, request, 0o600); err != nil {
		return nil, err
	}
	return request, nil
}

func (a *Authorizer) createPostWorker(completion PostWorkerCompletion, mode string) (*Request, bool, error) {
	if err := a.ensureDir(); err != nil {
		return nil, false, err
	}
	identity := strings.Join([]string{string(OriginPostWorker), completion.BeadID, completion.RigName, completion.MRID, mode}, "\x00")
	hash := sha256.Sum256([]byte(identity))
	id := hex.EncodeToString(hash[:16])
	path, err := a.requestPath(id)
	if err != nil {
		return nil, false, err
	}
	unlock, err := lock.FlockAcquire(path + ".lock")
	if err != nil {
		return nil, false, err
	}
	defer unlock()
	if existing, getErr := a.Get(id); getErr == nil {
		if existing.Origin != OriginPostWorker || existing.BeadID != completion.BeadID || existing.RigName != completion.RigName || existing.MRID != completion.MRID || existing.Mode != mode {
			return nil, false, fmt.Errorf("Assayer post-worker request identity collision for %s", id)
		}
		return existing, false, nil
	} else if !os.IsNotExist(getErr) {
		return nil, false, getErr
	}
	request := &Request{
		ID: id, BeadID: completion.BeadID, RigName: completion.RigName, Mode: mode,
		Origin: OriginPostWorker, PolecatName: completion.PolecatName, Branch: completion.Branch, MRID: completion.MRID,
		State: StateRequested, RequestedAt: a.now().UTC(),
	}
	if err := atomicfile.WriteJSONWithPerm(path, request, 0o600); err != nil {
		return nil, false, err
	}
	return request, true, nil
}

// Authorize grants a short-lived capability from a neutral human shell.
func (a *Authorizer) Authorize(id string, ttl time.Duration) (string, *Request, error) {
	if err := RequireNeutralHumanShell(); err != nil {
		return "", nil, err
	}
	policy, err := LoadPolicy(a.townRoot)
	if err != nil {
		return "", nil, err
	}
	maximumTTL, err := time.ParseDuration(policy.AuthorizationTTL)
	if err != nil {
		return "", nil, fmt.Errorf("parsing Assayer authorization TTL: %w", err)
	}
	if ttl <= 0 {
		ttl = maximumTTL
	} else if ttl > maximumTTL {
		return "", nil, fmt.Errorf("Assayer authorization TTL exceeds the configured Phase 1 limit of %s", maximumTTL)
	}
	token, err := randomHex(32)
	if err != nil {
		return "", nil, err
	}
	var result *Request
	err = a.update(id, func(request *Request) error {
		if request.State != StateRequested {
			return fmt.Errorf("%w: request state is %s", ErrConsumed, request.State)
		}
		now := a.now().UTC()
		expires := now.Add(ttl)
		hash := sha256.Sum256([]byte(token))
		request.TokenHash = hex.EncodeToString(hash[:])
		request.AuthorizedBy = "human"
		request.AuthorizedAt, request.ExpiresAt, request.State = &now, &expires, StateAuthorized
		copy := *request
		result = &copy
		return nil
	})
	return token, result, err
}

// ConsumeMayor lets the trusted Town Mayor authorize and consume a requested
// run in one auditable transition. Other agent roles cannot use this path.
func (a *Authorizer) ConsumeMayor(id string) (*Request, error) {
	if err := RequireMayorSession(); err != nil {
		return nil, err
	}
	policy, err := LoadPolicy(a.townRoot)
	if err != nil {
		return nil, err
	}
	if !policy.Authority.MayorRunAuthorization {
		return nil, fmt.Errorf("%w: Mayor run authorization is disabled by Assayer policy", ErrUnauthorized)
	}
	var result *Request
	err = a.update(id, func(request *Request) error {
		if request.State != StateRequested {
			return fmt.Errorf("%w: request state is %s", ErrConsumed, request.State)
		}
		now := a.now().UTC()
		request.TokenHash = ""
		request.AuthorizedBy = constants.RoleMayor
		request.AuthorizedAt = &now
		request.ExpiresAt = nil
		request.ConsumedAt = &now
		request.State = StateConsumed
		copy := *request
		result = &copy
		return nil
	})
	return result, err
}

// consumeLifecycleMayor is reserved for native lifecycle integrations. It
// applies the same policy gate and audit identity as Mayor without trusting an
// agent-controlled environment variable.
func (a *Authorizer) consumeLifecycleMayor(id string) (*Request, error) {
	policy, err := LoadPolicy(a.townRoot)
	if err != nil {
		return nil, err
	}
	if !policy.Authority.MayorRunAuthorization {
		return nil, fmt.Errorf("%w: Mayor run authorization is disabled by Assayer policy", ErrUnauthorized)
	}
	var result *Request
	err = a.update(id, func(request *Request) error {
		if request.State != StateRequested {
			return fmt.Errorf("%w: request state is %s", ErrConsumed, request.State)
		}
		now := a.now().UTC()
		request.TokenHash = ""
		request.AuthorizedBy = constants.RoleMayor
		request.AuthorizedAt = &now
		request.ExpiresAt = nil
		request.ConsumedAt = &now
		request.State = StateConsumed
		copy := *request
		result = &copy
		return nil
	})
	return result, err
}

// RequireMayorSession recognizes only the native Mayor role environment.
func RequireMayorSession() error {
	role := strings.TrimSpace(os.Getenv("GT_ROLE"))
	actor := strings.TrimSpace(os.Getenv("BD_ACTOR"))
	if role != "" && role != constants.RoleMayor {
		return fmt.Errorf("%w: conflicting agent role %s", ErrUnauthorized, role)
	}
	if actor != "" && actor != constants.RoleMayor {
		return fmt.Errorf("%w: conflicting Beads actor %s", ErrUnauthorized, actor)
	}
	if role == constants.RoleMayor || actor == constants.RoleMayor {
		return nil
	}
	return fmt.Errorf("%w: operation requires the native Mayor role", ErrUnauthorized)
}

// RequireNeutralHumanShell rejects known Gas Town and Beads agent identities.
// It is shared by all human-owned Assayer policy and authorization mutations.
func RequireNeutralHumanShell() error {
	for _, name := range []string{"GT_ROLE", "GT_SESSION", "GT_POLECAT", "GT_CREW", "BD_ACTOR", "BEADS_AGENT_NAME"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return fmt.Errorf("%w: operation must run outside an agent session (%s=%s)", ErrUnauthorized, name, value)
		}
	}
	return nil
}

// Consume atomically burns the capability before orchestration begins.
func (a *Authorizer) Consume(id, token string) (*Request, error) {
	var result *Request
	err := a.update(id, func(request *Request) error {
		if request.State != StateAuthorized {
			return fmt.Errorf("%w: request state is %s", ErrConsumed, request.State)
		}
		now := a.now().UTC()
		if request.ExpiresAt == nil || !now.Before(*request.ExpiresAt) {
			return ErrExpired
		}
		provided := sha256.Sum256([]byte(token))
		expected, decodeErr := hex.DecodeString(request.TokenHash)
		if decodeErr != nil || len(expected) != sha256.Size || subtle.ConstantTimeCompare(provided[:], expected) != 1 {
			return ErrUnauthorized
		}
		request.TokenHash, request.State, request.ConsumedAt = "", StateConsumed, &now
		copy := *request
		result = &copy
		return nil
	})
	return result, err
}

func (a *Authorizer) Get(id string) (*Request, error) {
	path, err := a.requestPath(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var request Request
	if err := json.Unmarshal(data, &request); err != nil {
		return nil, err
	}
	return &request, nil
}

// MarkDeliveryFailed seals a consumed request when the activation notice could
// not be delivered. The capability remains burned and the terminal reason is
// inspectable; a retry requires a fresh request and authorization.
func (a *Authorizer) MarkDeliveryFailed(id string, deliveryErr error) error {
	return a.update(id, func(request *Request) error {
		if request.State != StateConsumed {
			return fmt.Errorf("cannot record delivery failure from state %s", request.State)
		}
		request.State = StateDeliveryFailed
		request.DeliveryError = deliveryErr.Error()
		return nil
	})
}

func (a *Authorizer) update(id string, fn func(*Request) error) error {
	if err := a.ensureDir(); err != nil {
		return err
	}
	path, err := a.requestPath(id)
	if err != nil {
		return err
	}
	unlock, err := lock.FlockAcquire(path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	request, err := a.Get(id)
	if err != nil {
		return err
	}
	if err := fn(request); err != nil {
		return err
	}
	return atomicfile.WriteJSONWithPerm(path, request, 0o600)
}

func randomHex(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
