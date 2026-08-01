package assayer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

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
	townRoot := t.TempDir()
	if err := EnsureHome(townRoot, ""); err != nil {
		t.Fatal(err)
	}
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
	if request.State != StateConsumed || request.AuthorizedBy != "mayor" || request.Origin != OriginPostWorker {
		t.Fatalf("unexpected activated request: %#v", request)
	}
	if request.BeadID != "api-123" || request.RigName != "api" || request.MRID != "api-mr-456" {
		t.Fatalf("request lost completion identity: %#v", request)
	}
}

func TestPostWorkerCompletionIsIdempotentForSameMergeRequest(t *testing.T) {
	townRoot := t.TempDir()
	if err := EnsureHome(townRoot, ""); err != nil {
		t.Fatal(err)
	}
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
	townRoot := t.TempDir()
	if err := EnsureHome(townRoot, ""); err != nil {
		t.Fatal(err)
	}
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
	townRoot := t.TempDir()
	if err := EnsureHome(townRoot, ""); err != nil {
		t.Fatal(err)
	}
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
}

func TestPostWorkerCompletionWithoutMergeRequestIsSkipped(t *testing.T) {
	townRoot := t.TempDir()
	if err := EnsureHome(townRoot, ""); err != nil {
		t.Fatal(err)
	}
	sink := &recordingActivationSink{running: true}
	result, err := newPostWorkerDispatcher(townRoot, sink).Dispatch(PostWorkerCompletion{BeadID: "api-123", RigName: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Activated || result.Reason != "merge request required" || sink.deliveryCount() != 0 {
		t.Fatalf("non-MR completion was not skipped: result=%#v deliveries=%d", result, sink.deliveryCount())
	}
}
