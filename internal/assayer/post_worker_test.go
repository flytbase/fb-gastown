package assayer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newPostWorkerTestTown(t *testing.T) string {
	t.Helper()
	townRoot := t.TempDir()
	if err := EnsureHome(townRoot, ""); err != nil {
		t.Fatal(err)
	}
	policy := DefaultPolicy()
	policy.Automation.PostWorker.Enabled = true
	policy.Automation.PostWorker.EligibleRigs = []string{"api"}
	data, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(townRoot, "assayer", "assayer.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return townRoot
}

type recordingActivationSink struct {
	running     bool
	delivered   []*Request
	runningErr  error
	deliveryErr error
	mu          sync.Mutex
}

func (s *recordingActivationSink) IsRunning() (bool, error) {
	return s.running, s.runningErr
}

func (s *recordingActivationSink) Deliver(request *Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deliveryErr != nil {
		return s.deliveryErr
	}
	s.delivered = append(s.delivered, request)
	return nil
}

func (s *recordingActivationSink) deliveryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.delivered)
}

func TestPostWorkerCompletionActivatesMayorAuthorizedAdvisoryRun(t *testing.T) {
	townRoot := newPostWorkerTestTown(t)
	sink := &recordingActivationSink{running: true}
	dispatcher := newPostWorkerDispatcher(townRoot, sink)

	result, err := dispatcher.Dispatch(PostWorkerCompletion{
		BeadID:      "api-123",
		RigName:     "api",
		PolecatName: "furiosa",
		Branch:      "polecat/furiosa/api-123",
		MRID:        "api-mr-456",
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !result.Activated || result.RequestID == "" || result.Advisory != true {
		t.Fatalf("unexpected dispatch result: %#v", result)
	}
	if sink.deliveryCount() != 1 {
		t.Fatalf("delivered requests = %d, want 1", sink.deliveryCount())
	}
	request := sink.delivered[0]
	if request.State != StateDelivering || request.AuthorizedBy != "mayor" || request.Origin != OriginPostWorker {
		t.Fatalf("unexpected activated request: %#v", request)
	}
	if request.BeadID != "api-123" || request.RigName != "api" || request.MRID != "api-mr-456" {
		t.Fatalf("request lost completion identity: %#v", request)
	}
	stored, err := NewAuthorizer(townRoot).Get(result.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != StateDelivered {
		t.Fatalf("stored request state = %s, want %s", stored.State, StateDelivered)
	}
}

func TestPostWorkerCompletionIsIdempotentForSameMergeRequest(t *testing.T) {
	townRoot := newPostWorkerTestTown(t)
	sink := &recordingActivationSink{running: true}
	dispatcher := newPostWorkerDispatcher(townRoot, sink)
	completion := PostWorkerCompletion{
		BeadID:      "api-123",
		RigName:     "api",
		PolecatName: "furiosa",
		Branch:      "polecat/furiosa/api-123",
		MRID:        "api-mr-456",
	}

	first, err := dispatcher.Dispatch(completion)
	if err != nil {
		t.Fatal(err)
	}
	second, err := dispatcher.Dispatch(completion)
	if err != nil {
		t.Fatal(err)
	}
	if first.RequestID != second.RequestID {
		t.Fatalf("duplicate completion created two requests: %s != %s", first.RequestID, second.RequestID)
	}
	if sink.deliveryCount() != 1 {
		t.Fatalf("duplicate completion delivered %d activations, want 1", sink.deliveryCount())
	}
	if second.Activated || second.Reason != "already activated" {
		t.Fatalf("duplicate dispatch was not reported as idempotent: %#v", second)
	}
}

func TestPostWorkerCompletionIsIdempotentUnderConcurrentDelivery(t *testing.T) {
	townRoot := newPostWorkerTestTown(t)
	sink := &recordingActivationSink{running: true}
	dispatcher := newPostWorkerDispatcher(townRoot, sink)
	completion := PostWorkerCompletion{BeadID: "api-123", RigName: "api", Branch: "feature/api-123", MRID: "api-mr-456"}

	const attempts = 12
	results := make(chan *PostWorkerDispatchResult, attempts)
	errors := make(chan error, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := dispatcher.Dispatch(completion)
			results <- result
			errors <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errors)

	for err := range errors {
		if err != nil {
			t.Errorf("concurrent Dispatch() error = %v", err)
		}
	}
	var requestID string
	for result := range results {
		if result == nil {
			continue
		}
		if requestID == "" {
			requestID = result.RequestID
		} else if result.RequestID != requestID {
			t.Errorf("concurrent request id = %s, want %s", result.RequestID, requestID)
		}
	}
	if sink.deliveryCount() != 1 {
		t.Fatalf("concurrent completion delivered %d activations, want 1", sink.deliveryCount())
	}
}

func TestPostWorkerDeliveryFailureIsSealedForAudit(t *testing.T) {
	townRoot := newPostWorkerTestTown(t)
	sink := &recordingActivationSink{running: true, deliveryErr: errors.New("session unavailable")}
	dispatcher := newPostWorkerDispatcher(townRoot, sink)

	_, err := dispatcher.Dispatch(PostWorkerCompletion{BeadID: "api-123", RigName: "api", MRID: "api-mr-456"})
	if err == nil || !strings.Contains(err.Error(), "session unavailable") {
		t.Fatalf("Dispatch() error = %v, want delivery failure", err)
	}
	entries, err := os.ReadDir(filepath.Join(townRoot, ".runtime", "assayer", "requests"))
	if err != nil {
		t.Fatal(err)
	}
	var requestID string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			requestID = strings.TrimSuffix(entry.Name(), ".json")
		}
	}
	if requestID == "" {
		t.Fatal("post-worker request was not persisted")
	}
	request, err := NewAuthorizer(townRoot).Get(requestID)
	if err != nil {
		t.Fatal(err)
	}
	if request.State != StateDeliveryFailed || request.DeliveryError != "session unavailable" || request.AuthorizedBy != "mayor" {
		t.Fatalf("delivery failure was not sealed: %#v", request)
	}
	sink.deliveryErr = nil
	delivered, err := dispatcher.DeliverPending()
	if err != nil {
		t.Fatalf("DeliverPending() retry error = %v", err)
	}
	if delivered != 1 || sink.deliveryCount() != 1 {
		t.Fatalf("delivery failure was not retried: delivered=%d sink=%d", delivered, sink.deliveryCount())
	}
	request, err = NewAuthorizer(townRoot).Get(requestID)
	if err != nil {
		t.Fatal(err)
	}
	if request.State != StateDelivered || request.DeliveryError != "" {
		t.Fatalf("retried request was not delivered: %#v", request)
	}
}

func TestPostWorkerCompletionWithoutMergeRequestIsSkipped(t *testing.T) {
	townRoot := newPostWorkerTestTown(t)
	sink := &recordingActivationSink{running: true}
	result, err := newPostWorkerDispatcher(townRoot, sink).Dispatch(PostWorkerCompletion{BeadID: "api-123", RigName: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Activated || result.Reason != "merge request required" || sink.deliveryCount() != 0 {
		t.Fatalf("non-MR completion was not skipped: result=%#v deliveries=%d", result, sink.deliveryCount())
	}
}

func TestPostWorkerCompletionOutsideExplicitBackendRigsIsSkipped(t *testing.T) {
	townRoot := newPostWorkerTestTown(t)
	sink := &recordingActivationSink{running: true}
	result, err := newPostWorkerDispatcher(townRoot, sink).Dispatch(PostWorkerCompletion{BeadID: "web-123", RigName: "frontend", MRID: "web-mr-456"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Activated || result.Reason != "rig frontend is not eligible for backend assurance" || sink.deliveryCount() != 0 {
		t.Fatalf("non-backend rig was not skipped: result=%#v deliveries=%d", result, sink.deliveryCount())
	}
}

func TestPostWorkerCompletionQueuesWhileAssayerIsStopped(t *testing.T) {
	townRoot := newPostWorkerTestTown(t)
	sink := &recordingActivationSink{running: false}
	dispatcher := newPostWorkerDispatcher(townRoot, sink)
	completion := PostWorkerCompletion{BeadID: "api-123", RigName: "api", MRID: "api-mr-456"}

	queued, err := dispatcher.Dispatch(completion)
	if err != nil {
		t.Fatalf("Dispatch() while stopped error = %v", err)
	}
	if !queued.Queued || queued.RequestID == "" || queued.Reason != "waiting for Assayer session" {
		t.Fatalf("completion was not durably queued: %#v", queued)
	}
	request, err := NewAuthorizer(townRoot).Get(queued.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if request.State != StateAuthorized || request.AuthorizedBy != "mayor" {
		t.Fatalf("queued request is not Mayor-authorized: %#v", request)
	}

	sink.running = true
	deliveredCount, err := dispatcher.DeliverPending()
	if err != nil {
		t.Fatalf("DeliverPending() error = %v", err)
	}
	if deliveredCount != 1 || sink.deliveryCount() != 1 {
		t.Fatalf("queued request was not delivered after restart: count=%d deliveries=%d", deliveredCount, sink.deliveryCount())
	}
	request, err = NewAuthorizer(townRoot).Get(queued.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if request.State != StateDelivered {
		t.Fatalf("delivered request state = %s, want %s", request.State, StateDelivered)
	}
}

func TestAssayerClaimsAndCompletesDeliveredPostWorkerRunExactlyOnce(t *testing.T) {
	t.Setenv("GT_ROLE", "assayer")
	t.Setenv("BD_ACTOR", "")
	townRoot := newPostWorkerTestTown(t)
	sink := &recordingActivationSink{running: true}
	result, err := newPostWorkerDispatcher(townRoot, sink).Dispatch(PostWorkerCompletion{BeadID: "api-123", RigName: "api", MRID: "api-mr-456"})
	if err != nil {
		t.Fatal(err)
	}
	authorizer := NewAuthorizer(townRoot)
	claimed, err := authorizer.ClaimPostWorker(result.RequestID)
	if err != nil {
		t.Fatalf("ClaimPostWorker() error = %v", err)
	}
	if claimed.State != StateConsumed || claimed.ConsumedAt == nil {
		t.Fatalf("delivered request was not claimed: %#v", claimed)
	}
	if _, err := authorizer.ClaimPostWorker(result.RequestID); !errors.Is(err, ErrConsumed) {
		t.Fatalf("second ClaimPostWorker() error = %v, want ErrConsumed", err)
	}
	completed, err := authorizer.CompletePostWorker(result.RequestID, OutcomeAdvisoryComplete)
	if err != nil {
		t.Fatalf("CompletePostWorker() error = %v", err)
	}
	if completed.State != StateCompleted || completed.Outcome != OutcomeAdvisoryComplete || completed.CompletedAt == nil {
		t.Fatalf("claimed request was not completed: %#v", completed)
	}
}

func TestClaimedPostWorkerRunIsRequeuedAfterAssayerRestart(t *testing.T) {
	t.Setenv("GT_ROLE", "assayer")
	t.Setenv("BD_ACTOR", "")
	townRoot := newPostWorkerTestTown(t)
	sink := &recordingActivationSink{running: true}
	dispatcher := newPostWorkerDispatcher(townRoot, sink)
	result, err := dispatcher.Dispatch(PostWorkerCompletion{BeadID: "api-123", RigName: "api", MRID: "api-mr-456"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewAuthorizer(townRoot).ClaimPostWorker(result.RequestID); err != nil {
		t.Fatal(err)
	}

	delivered, err := dispatcher.DeliverPendingAfterRestart()
	if err != nil {
		t.Fatalf("DeliverPendingAfterRestart() error = %v", err)
	}
	if delivered != 1 || sink.deliveryCount() != 2 {
		t.Fatalf("claimed run was not redelivered after restart: delivered=%d sink=%d", delivered, sink.deliveryCount())
	}
	claimed, err := NewAuthorizer(townRoot).ClaimPostWorker(result.RequestID)
	if err != nil {
		t.Fatalf("recovered ClaimPostWorker() error = %v", err)
	}
	if claimed.State != StateConsumed {
		t.Fatalf("recovered request state = %s, want %s", claimed.State, StateConsumed)
	}
}
