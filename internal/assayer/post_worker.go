package assayer

import (
	"errors"
	"fmt"
	"strings"

	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/tmux"
)

// PostWorkerCompletion is the stable identity of one completed Polecat
// submission. Phase 1 accepts only completed work with a merge request.
type PostWorkerCompletion struct {
	BeadID      string
	RigName     string
	PolecatName string
	Branch      string
	MRID        string
}

// PostWorkerDispatchResult describes the native advisory handoff. Assayer
// never becomes a merge gate while real backend execution is disabled.
type PostWorkerDispatchResult struct {
	Activated bool
	Advisory  bool
	RequestID string
	Reason    string
}

type activationSink interface {
	IsRunning() (bool, error)
	Deliver(*Request) error
}

type sessionActivationSink struct {
	townRoot string
}

func (s *sessionActivationSink) IsRunning() (bool, error) {
	return NewManager(s.townRoot).IsRunning()
}

func (s *sessionActivationSink) Deliver(request *Request) error {
	notice := fmt.Sprintf("Native post-worker Assayer advisory %s is ready for bead %s in rig %s (MR %s). Process it now and inspect the sealed envelope with: gt assayer inspect %s", request.ID, request.BeadID, request.RigName, request.MRID, request.ID)
	return tmux.NewTmux().NudgeSessionWithOpts(session.AssayerSessionName(), notice, tmux.NudgeOpts{TownRoot: s.townRoot})
}

type postWorkerDispatcher struct {
	townRoot string
	sink     activationSink
}

func newPostWorkerDispatcher(townRoot string, sink activationSink) *postWorkerDispatcher {
	return &postWorkerDispatcher{townRoot: townRoot, sink: sink}
}

// DispatchPostWorker performs the complete native post-worker activation.
// Callers need not create requests, impersonate Mayor, or manage sessions.
func DispatchPostWorker(townRoot string, completion PostWorkerCompletion) (*PostWorkerDispatchResult, error) {
	return newPostWorkerDispatcher(townRoot, &sessionActivationSink{townRoot: townRoot}).Dispatch(completion)
}

func (d *postWorkerDispatcher) Dispatch(completion PostWorkerCompletion) (*PostWorkerDispatchResult, error) {
	policy, err := LoadPolicy(d.townRoot)
	if err != nil {
		return nil, err
	}
	result := &PostWorkerDispatchResult{Advisory: !policy.Authority.RealBackendExecution}
	if !policy.Enabled || !policy.Automation.PostWorker.Enabled {
		result.Reason = "post-worker automation disabled"
		return result, nil
	}
	completion.BeadID = strings.TrimSpace(completion.BeadID)
	completion.RigName = strings.TrimSpace(completion.RigName)
	completion.MRID = strings.TrimSpace(completion.MRID)
	if completion.BeadID == "" || completion.RigName == "" {
		return nil, fmt.Errorf("post-worker Assayer dispatch requires bead id and rig name")
	}
	if policy.Automation.PostWorker.RequireMergeRequest && completion.MRID == "" {
		result.Reason = "merge request required"
		return result, nil
	}
	running, err := d.sink.IsRunning()
	if err != nil {
		return nil, fmt.Errorf("checking Assayer session: %w", err)
	}
	if !running {
		return nil, ErrNotRunning
	}
	authorizer := NewAuthorizer(d.townRoot)
	request, created, err := authorizer.createPostWorker(completion, policy.Automation.PostWorker.Mode)
	if err != nil {
		return nil, err
	}
	result.RequestID = request.ID
	if !created && request.State != StateRequested {
		if request.State == StateConsumed {
			result.Reason = "already activated"
		} else {
			result.Reason = fmt.Sprintf("existing request is %s", request.State)
		}
		return result, nil
	}
	requestID := request.ID
	request, err = authorizer.consumeLifecycleMayor(requestID)
	if err != nil {
		if errors.Is(err, ErrConsumed) {
			stored, getErr := authorizer.Get(requestID)
			if getErr == nil && (stored.State == StateConsumed || stored.State == StateDeliveryFailed) {
				result.Reason = "already activated"
				return result, nil
			}
		}
		return nil, err
	}
	if err := d.sink.Deliver(request); err != nil {
		if recordErr := authorizer.MarkDeliveryFailed(request.ID, err); recordErr != nil {
			return nil, fmt.Errorf("delivering Assayer activation: %v (also failed to seal delivery failure: %w)", err, recordErr)
		}
		return nil, err
	}
	result.Activated = true
	return result, nil
}
