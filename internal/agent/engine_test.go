package agent

import (
	"context"
	"sync"
	"testing"
)

func TestEvaluationSuite(t *testing.T) {
	report := RunEvaluation(context.Background())
	if report.Passed != report.Total {
		t.Fatalf("expected all cases to pass, got %d/%d: %#v", report.Passed, report.Total, report.Cases)
	}
}

func TestIdempotencyReturnsOriginalRun(t *testing.T) {
	engine := NewEngine(nil)
	incident := Incident{
		ID:       "inc-1",
		Service:  "checkout-api",
		Severity: "SEV-1",
		Symptom:  "timeout",
		Scenario: "payment-timeout",
	}

	first, duplicate, err := engine.Run(context.Background(), incident, "stable-key")
	if err != nil || duplicate {
		t.Fatalf("unexpected first run result: duplicate=%v err=%v", duplicate, err)
	}
	second, duplicate, err := engine.Run(context.Background(), incident, "stable-key")
	if err != nil || !duplicate {
		t.Fatalf("expected duplicate result: duplicate=%v err=%v", duplicate, err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotency mismatch: %s != %s", first.ID, second.ID)
	}
	if got := engine.Metrics().Deduplicated; got != 1 {
		t.Fatalf("expected one deduplicated call, got %d", got)
	}
}

func TestCircuitBreakerShortCircuitsSecondOutage(t *testing.T) {
	engine := NewEngine(nil)
	incident := Incident{
		Service:  "payment-router",
		Severity: "SEV-1",
		Symptom:  "connection refused",
		Scenario: "provider-outage",
	}
	if _, _, err := engine.Run(context.Background(), incident, "outage-1"); err != nil {
		t.Fatal(err)
	}
	second, _, err := engine.Run(context.Background(), incident, "outage-2")
	if err != nil {
		t.Fatal(err)
	}
	if second.Steps[2].Status != "short_circuited" {
		t.Fatalf("expected short circuit, got %#v", second.Steps[2])
	}
}

func TestConcurrentIdempotencyWaitsForOriginalRun(t *testing.T) {
	engine := NewEngine(nil)
	incident := Incident{
		ID:       "inc-concurrent",
		Service:  "checkout-api",
		Severity: "SEV-1",
		Symptom:  "timeout",
		Scenario: "payment-timeout",
	}

	const callers = 12
	var wg sync.WaitGroup
	wg.Add(callers)
	runIDs := make(chan string, callers)
	duplicateCount := 0
	var duplicateMu sync.Mutex

	for range callers {
		go func() {
			defer wg.Done()
			run, duplicate, err := engine.Run(context.Background(), incident, "concurrent-key")
			if err != nil {
				t.Errorf("run failed: %v", err)
				return
			}
			if run == nil {
				t.Error("run must not be nil")
				return
			}
			runIDs <- run.ID
			if duplicate {
				duplicateMu.Lock()
				duplicateCount++
				duplicateMu.Unlock()
			}
		}()
	}
	wg.Wait()
	close(runIDs)

	var firstID string
	for runID := range runIDs {
		if firstID == "" {
			firstID = runID
		}
		if runID != firstID {
			t.Fatalf("expected one run id, got %s and %s", firstID, runID)
		}
	}
	if duplicateCount != callers-1 {
		t.Fatalf("expected %d duplicate callers, got %d", callers-1, duplicateCount)
	}
	if got := engine.Metrics().TotalRuns; got != 1 {
		t.Fatalf("expected one execution, got %d", got)
	}
}
