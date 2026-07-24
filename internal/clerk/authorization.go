// Package clerk implements the supervised Clerk control plane.
package clerk

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
	"github.com/steveyegge/gastown/internal/lock"
)

type RequestState string

const (
	StateRequested   RequestState = "requested"
	StateAuthorized  RequestState = "authorized"
	StateDispatching RequestState = "dispatching"
	StateDispatched  RequestState = "dispatched"
	StateFailed      RequestState = "failed"
)

var (
	ErrUnauthorized = errors.New("Clerk dispatch is not authorized")
	ErrExpired      = errors.New("Clerk dispatch authorization expired")
	ErrConsumed     = errors.New("Clerk dispatch authorization was already consumed")
)

// Request is the durable state machine for one supervised dispatch.
type Request struct {
	ID           string       `json:"id"`
	BeadID       string       `json:"bead_id"`
	RigName      string       `json:"rig_name"`
	State        RequestState `json:"state"`
	TokenHash    string       `json:"token_hash,omitempty"`
	RequestedAt  time.Time    `json:"requested_at"`
	AuthorizedAt *time.Time   `json:"authorized_at,omitempty"`
	ExpiresAt    *time.Time   `json:"expires_at,omitempty"`
	ConsumedAt   *time.Time   `json:"consumed_at,omitempty"`
	CompletedAt  *time.Time   `json:"completed_at,omitempty"`
	Error        string       `json:"error,omitempty"`
}

// Authorizer persists one-use dispatch capabilities under the Town runtime.
type Authorizer struct {
	townRoot string
	now      func() time.Time
}

func NewAuthorizer(townRoot string) *Authorizer {
	return &Authorizer{townRoot: townRoot, now: time.Now}
}

func (a *Authorizer) requestsDir() string {
	return filepath.Join(a.townRoot, ".runtime", "clerk", "requests")
}

func (a *Authorizer) requestPath(id string) (string, error) {
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return "", fmt.Errorf("invalid Clerk request id %q", id)
	}
	return filepath.Join(a.requestsDir(), id+".json"), nil
}

func (a *Authorizer) ensureDir() error {
	if err := os.MkdirAll(a.requestsDir(), 0700); err != nil {
		return err
	}
	return os.Chmod(filepath.Join(a.townRoot, ".runtime", "clerk"), 0700)
}

// Create binds a new request to exactly one bead and rig.
func (a *Authorizer) Create(beadID, rigName string) (*Request, error) {
	if strings.TrimSpace(beadID) == "" || strings.TrimSpace(rigName) == "" {
		return nil, fmt.Errorf("bead id and rig name are required")
	}
	if err := a.ensureDir(); err != nil {
		return nil, fmt.Errorf("creating Clerk request directory: %w", err)
	}
	id, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	request := &Request{
		ID:          id,
		BeadID:      beadID,
		RigName:     rigName,
		State:       StateRequested,
		RequestedAt: a.now().UTC(),
	}
	path, _ := a.requestPath(id)
	if err := atomicfile.WriteJSONWithPerm(path, request, 0600); err != nil {
		return nil, fmt.Errorf("writing Clerk request: %w", err)
	}
	return request, nil
}

// Authorize grants a short-lived, one-use capability. It must be called from a
// neutral human shell, never from an agent role.
func (a *Authorizer) Authorize(id string, ttl time.Duration) (string, *Request, error) {
	if role := strings.TrimSpace(os.Getenv("GT_ROLE")); role != "" {
		return "", nil, fmt.Errorf("%w: authorization must run outside an agent session (GT_ROLE=%s)", ErrUnauthorized, role)
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
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
		request.AuthorizedAt = &now
		request.ExpiresAt = &expires
		request.State = StateAuthorized
		result = cloneRequest(request)
		return nil
	})
	return token, result, err
}

// Consume atomically burns a capability before any worker/worktree mutation.
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
		expected, err := hex.DecodeString(request.TokenHash)
		if err != nil || len(expected) != sha256.Size ||
			subtle.ConstantTimeCompare(provided[:], expected) != 1 {
			return ErrUnauthorized
		}
		request.TokenHash = ""
		request.ConsumedAt = &now
		request.State = StateDispatching
		result = cloneRequest(request)
		return nil
	})
	return result, err
}

// Complete records the outcome after native executeSling returns.
func (a *Authorizer) Complete(id string, dispatchErr error) error {
	return a.update(id, func(request *Request) error {
		if request.State != StateDispatching {
			return fmt.Errorf("cannot complete Clerk request in state %s", request.State)
		}
		now := a.now().UTC()
		request.CompletedAt = &now
		if dispatchErr != nil {
			request.State = StateFailed
			request.Error = dispatchErr.Error()
		} else {
			request.State = StateDispatched
			request.Error = ""
		}
		return nil
	})
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
	return atomicfile.WriteJSONWithPerm(path, request, 0600)
}

func randomHex(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func cloneRequest(request *Request) *Request {
	copy := *request
	return &copy
}
