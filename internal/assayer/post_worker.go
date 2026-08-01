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
	Queued    bool
	Delivered bool
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
	notice := fmt.Sprintf("Native post-worker Assayer advisory %s is ready for bead %s in rig %s (MR %s). Atomically claim it with `gt assayer claim %s`; only the successful claimant may process it. Then inspect the sealed envelope with `gt assayer inspect %s`.", request.ID, request.BeadID, request.RigName, request.MRID, request.ID, request.ID)
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
	if !contains(policy.Automation.PostWorker.EligibleRigs, completion.RigName) {
		result.Reason = fmt.Sprintf("rig %s is not eligible for backend assurance", completion.RigName)
		return result, nil
	}
	if completion.BeadID == "" || completion.RigName == "" {
		return nil, fmt.Errorf("post-worker Assayer dispatch requires bead id and rig name")
	}
	if policy.Automation.PostWorker.RequireMergeRequest && completion.MRID == "" {
		result.Reason = "merge request required"
		return result, nil
	}
	authorizer := NewAuthorizer(d.townRoot)
	request, created, err := authorizer.createPostWorker(completion, policy.Automation.PostWorker.Mode)
	if err != nil {
		return nil, err
	}
	result.RequestID = request.ID
	if created || request.State == StateRequested {
		requestID := request.ID
		request, err = authorizer.authorizeLifecycleMayor(requestID)
		if err != nil {
			if !errors.Is(err, ErrConsumed) {
				return nil, err
			}
			request, err = authorizer.Get(requestID)
			if err != nil {
				return nil, err
			}
		}
	}
	switch request.State {
	case StateDelivered, StateConsumed, StateCompleted:
		result.Reason = "already activated"
		return result, nil
	case StateDelivering:
		result.Queued = true
		result.Reason = "delivery in progress"
		return result, nil
	case StateAuthorized, StateDeliveryFailed:
		// Ready for the single native delivery owner below.
	default:
		return nil, fmt.Errorf("post-worker request %s is in unexpected state %s", request.ID, request.State)
	}
	running, err := d.sink.IsRunning()
	if err != nil {
		return nil, fmt.Errorf("checking Assayer session: %w", err)
	}
	if !running {
		result.Queued = true
		result.Reason = "waiting for Assayer session"
		return result, nil
	}
	request, started, err := authorizer.beginPostWorkerDelivery(request.ID)
	if err != nil {
		return nil, err
	}
	if !started {
		result.Queued = request.State == StateDelivering
		result.Reason = "already activated"
		return result, nil
	}
	if err := d.sink.Deliver(request); err != nil {
		if recordErr := authorizer.MarkDeliveryFailed(request.ID, err); recordErr != nil {
			return nil, fmt.Errorf("delivering Assayer activation: %v (also failed to seal delivery failure: %w)", err, recordErr)
		}
		return nil, err
	}
	if err := authorizer.markPostWorkerDelivered(request.ID); err != nil {
		return nil, err
	}
	result.Activated = true
	result.Delivered = true
	return result, nil
}

// DeliverPending replays durable post-worker activations when the Assayer
// session starts. Delivered-but-unclaimed requests are safe to redeliver because
// ClaimPostWorker is the single atomic execution gate.
func (d *postWorkerDispatcher) DeliverPending() (int, error) {
	return d.deliverPending(false)
}

func (d *postWorkerDispatcher) DeliverPendingAfterRestart() (int, error) {
	return d.deliverPending(true)
}

func (d *postWorkerDispatcher) deliverPending(recoverClaims bool) (int, error) {
	policy, err := LoadPolicy(d.townRoot)
	if err != nil {
		return 0, err
	}
	if !policy.Enabled || !policy.Automation.PostWorker.Enabled {
		return 0, nil
	}
	running, err := d.sink.IsRunning()
	if err != nil {
		return 0, err
	}
	if !running {
		return 0, ErrNotRunning
	}
	authorizer := NewAuthorizer(d.townRoot)
	requests, err := authorizer.listRequests()
	if err != nil {
		return 0, err
	}
	delivered := 0
	var deliveryErrors []error
	for _, request := range requests {
		if request.Origin != OriginPostWorker {
			continue
		}
		switch request.State {
		case StateDelivering, StateDelivered:
			if !recoverClaims {
				continue
			}
			if err := authorizer.requeuePostWorker(request.ID, "Assayer session restarted before claim"); err != nil {
				deliveryErrors = append(deliveryErrors, err)
				continue
			}
		case StateConsumed:
			if !recoverClaims {
				continue
			}
			if err := authorizer.requeuePostWorker(request.ID, "Assayer session restarted before completion"); err != nil {
				deliveryErrors = append(deliveryErrors, err)
				continue
			}
		case StateAuthorized, StateDeliveryFailed:
			// Already queued for delivery.
		default:
			continue
		}
		result, dispatchErr := d.Dispatch(PostWorkerCompletion{
			BeadID: request.BeadID, RigName: request.RigName, PolecatName: request.PolecatName,
			Branch: request.Branch, MRID: request.MRID,
		})
		if dispatchErr != nil {
			deliveryErrors = append(deliveryErrors, fmt.Errorf("request %s: %w", request.ID, dispatchErr))
			continue
		}
		if result.Delivered {
			delivered++
		}
	}
	return delivered, errors.Join(deliveryErrors...)
}
