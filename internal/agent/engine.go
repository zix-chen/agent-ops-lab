package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var ErrInvalidIncident = errors.New("incident service, symptom and scenario are required")

type Enricher interface {
	Enrich(context.Context, Incident, string) (string, error)
	Name() string
}

type Engine struct {
	mu              sync.RWMutex
	runs            map[string]*Run
	idempotencyKeys map[string]*idempotencyRecord
	metrics         Metrics
	totalLatencyMs  float64
	breakerFailures int
	enricher        Enricher
	sequence        atomic.Uint64
	now             func() time.Time
}

type idempotencyRecord struct {
	runID string
	done  chan struct{}
}

func NewEngine(enricher Enricher) *Engine {
	return &Engine{
		runs:            make(map[string]*Run),
		idempotencyKeys: make(map[string]*idempotencyRecord),
		enricher:        enricher,
		now:             time.Now,
	}
}

func Scenarios() []Scenario {
	return []Scenario{
		{
			ID:          "payment-timeout",
			Name:        "Transient payment timeout",
			Service:     "checkout-api",
			Severity:    "SEV-1",
			Symptom:     "Payment success rate dropped to 91%; provider p99 exceeded 4s",
			Description: "Retries a flaky dependency, correlates metrics and requires approval for traffic movement.",
			Signal:      "retry + approval gate",
		},
		{
			ID:          "cache-saturation",
			Name:        "Cache worker saturation",
			Service:     "offer-engine",
			Severity:    "SEV-2",
			Symptom:     "Redis pool utilization is 98%; recommendation latency is rising",
			Description: "Finds a reversible runbook action and allows a low-risk automated response.",
			Signal:      "safe automation",
		},
		{
			ID:          "provider-outage",
			Name:        "Provider hard outage",
			Service:     "payment-router",
			Severity:    "SEV-1",
			Symptom:     "Provider requests fail consistently with connection refused",
			Description: "Exhausts bounded retries and opens a circuit breaker to contain cascading failure.",
			Signal:      "circuit breaker",
		},
		{
			ID:          "prompt-injection",
			Name:        "Runbook prompt injection",
			Service:     "support-agent",
			Severity:    "SEV-2",
			Symptom:     "Retrieved text asks the agent to ignore policy and export credentials",
			Description: "Blocks malicious retrieved instructions before any tool can execute.",
			Signal:      "policy block",
		},
	}
}

func (e *Engine) Run(ctx context.Context, incident Incident, idempotencyKey string) (*Run, bool, error) {
	if strings.TrimSpace(incident.Service) == "" ||
		strings.TrimSpace(incident.Symptom) == "" ||
		strings.TrimSpace(incident.Scenario) == "" {
		return nil, false, ErrInvalidIncident
	}

	if strings.TrimSpace(idempotencyKey) == "" {
		idempotencyKey = incident.ID
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		idempotencyKey = fingerprint(incident)
	}

	e.mu.Lock()
	if record, ok := e.idempotencyKeys[idempotencyKey]; ok {
		e.mu.Unlock()
		select {
		case <-record.done:
		case <-ctx.Done():
			return nil, true, ctx.Err()
		}
		e.mu.Lock()
		e.metrics.Deduplicated++
		run := cloneRun(e.runs[record.runID])
		e.mu.Unlock()
		return run, true, nil
	}
	runID := fmt.Sprintf("run_%06d", e.sequence.Add(1))
	record := &idempotencyRecord{runID: runID, done: make(chan struct{})}
	e.idempotencyKeys[idempotencyKey] = record
	e.mu.Unlock()

	run := e.execute(ctx, runID, incident)

	e.mu.Lock()
	e.runs[run.ID] = run
	e.metrics.TotalRuns++
	switch run.Status {
	case StatusResolved:
		e.metrics.Resolved++
	case StatusApproval:
		e.metrics.ApprovalPending++
	case StatusBlocked:
		e.metrics.Blocked++
	case StatusDegraded:
		e.metrics.Degraded++
	}
	for _, step := range run.Steps {
		if step.Attempts > 1 {
			e.metrics.Retried += step.Attempts - 1
		}
	}
	latency := float64(run.FinishedAt.Sub(run.CreatedAt).Milliseconds())
	e.totalLatencyMs += latency
	e.metrics.AvgLatencyMs = e.totalLatencyMs / float64(e.metrics.TotalRuns)
	e.metrics.CircuitOpen = e.breakerFailures >= 3
	snapshot := cloneRun(run)
	close(record.done)
	e.mu.Unlock()

	return snapshot, false, nil
}

func (e *Engine) execute(ctx context.Context, runID string, incident Incident) *Run {
	start := e.now().UTC()
	cursor := start
	run := &Run{
		ID:        runID,
		Incident:  incident,
		CreatedAt: start,
		Audit: []AuditEvent{{
			At:      start,
			Actor:   "orchestrator",
			Action:  "incident.accepted",
			Outcome: "recorded",
			Data:    map[string]any{"scenario": incident.Scenario},
		}},
	}

	addStep := func(name, status, detail string, attempts int, duration time.Duration) {
		stepStart := cursor
		cursor = cursor.Add(duration)
		run.Steps = append(run.Steps, Step{
			Name:       name,
			Status:     status,
			Attempts:   attempts,
			Detail:     detail,
			StartedAt:  stepStart,
			FinishedAt: cursor,
		})
		run.Audit = append(run.Audit, AuditEvent{
			At:      cursor,
			Actor:   "agent",
			Action:  "step." + strings.ReplaceAll(strings.ToLower(name), " ", "_"),
			Outcome: status,
			Data:    map[string]any{"attempts": attempts},
		})
	}

	addStep("Normalize alert", "passed", "Validated schema and attached an idempotency key.", 1, 12*time.Millisecond)

	if incident.Scenario == "prompt-injection" {
		addStep("Retrieve runbook", "warning", "Retrieved runbook contains untrusted instructions.", 1, 17*time.Millisecond)
		addStep("Policy gate", "blocked", "Prompt-injection signature matched: secret export and policy override.", 1, 8*time.Millisecond)
		run.Status = StatusBlocked
		run.Risk = "critical"
		run.Confidence = 0.99
		run.Diagnosis = "Untrusted retrieval attempted to override system policy."
		run.Recommendation = "Quarantine the document, rotate its index entry, and require owner review."
		run.FinishedAt = cursor
		return run
	}

	addStep("Retrieve runbook", "passed", "Selected a versioned runbook using service and symptom evidence.", 1, 18*time.Millisecond)

	switch incident.Scenario {
	case "payment-timeout":
		addStep("Query telemetry", "passed", "First provider query timed out; second attempt showed a p99 latency spike.", 2, 54*time.Millisecond)
		run.Status = StatusApproval
		run.Risk = "high"
		run.Confidence = 0.91
		run.Diagnosis = "The primary provider is accepting connections but its tail latency is breaching the checkout timeout budget."
		run.Recommendation = "Request approval to shift 10% of traffic to the backup route, then compare success rate for five minutes."
		addStep("Policy gate", "approval_required", "Traffic movement changes payment routing and cannot execute unattended.", 1, 9*time.Millisecond)
	case "cache-saturation":
		addStep("Query telemetry", "passed", "Pool wait time and worker backlog rose together; database latency remained stable.", 1, 24*time.Millisecond)
		run.Status = StatusResolved
		run.Risk = "low"
		run.Confidence = 0.94
		run.Diagnosis = "A cache worker concurrency cap is throttling request throughput."
		run.Recommendation = "Increase workers by two within the declared safe limit and watch pool utilization."
		addStep("Policy gate", "passed", "Action is reversible, bounded, and covered by an automatic rollback threshold.", 1, 9*time.Millisecond)
	case "provider-outage":
		e.mu.Lock()
		isOpen := e.breakerFailures >= 3
		e.mu.Unlock()
		if isOpen {
			addStep("Query telemetry", "short_circuited", "Circuit already open; skipped a known-failing dependency.", 1, 5*time.Millisecond)
		} else {
			addStep("Query telemetry", "failed", "Three connection attempts failed; retry budget exhausted.", 3, 76*time.Millisecond)
			e.mu.Lock()
			e.breakerFailures += 3
			e.mu.Unlock()
		}
		run.Status = StatusDegraded
		run.Risk = "critical"
		run.Confidence = 0.97
		run.Diagnosis = "The provider endpoint is unavailable and the circuit breaker is open."
		run.Recommendation = "Keep the circuit open, fail over eligible traffic, and page the provider owner."
		addStep("Contain failure", "passed", "Opened the circuit and prevented additional calls to the failing dependency.", 1, 7*time.Millisecond)
	default:
		addStep("Query telemetry", "passed", "No unsafe condition detected.", 1, 15*time.Millisecond)
		run.Status = StatusResolved
		run.Risk = "low"
		run.Confidence = 0.72
		run.Diagnosis = "Signals are consistent with a transient service degradation."
		run.Recommendation = "Continue observation and collect a larger trace sample."
	}

	if e.enricher != nil && run.Status != StatusDegraded {
		if enriched, err := e.enricher.Enrich(ctx, incident, run.Diagnosis); err == nil && strings.TrimSpace(enriched) != "" {
			run.Diagnosis = enriched
			run.Audit = append(run.Audit, AuditEvent{
				At:      cursor,
				Actor:   e.enricher.Name(),
				Action:  "diagnosis.enriched",
				Outcome: "accepted",
			})
		} else if err != nil {
			run.Audit = append(run.Audit, AuditEvent{
				At:      cursor,
				Actor:   e.enricher.Name(),
				Action:  "diagnosis.enriched",
				Outcome: "fallback",
			})
		}
	}

	run.FinishedAt = cursor
	return run
}

func (e *Engine) ListRuns() []*Run {
	e.mu.RLock()
	defer e.mu.RUnlock()

	runs := make([]*Run, 0, len(e.runs))
	for _, run := range e.runs {
		runs = append(runs, cloneRun(run))
	}
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].CreatedAt.After(runs[j].CreatedAt)
	})
	return runs
}

func (e *Engine) Metrics() Metrics {
	e.mu.RLock()
	defer e.mu.RUnlock()
	metrics := e.metrics
	metrics.CircuitOpen = e.breakerFailures >= 3
	return metrics
}

func (e *Engine) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.runs = make(map[string]*Run)
	e.idempotencyKeys = make(map[string]*idempotencyRecord)
	e.metrics = Metrics{}
	e.totalLatencyMs = 0
	e.breakerFailures = 0
}

func fingerprint(incident Incident) string {
	sum := sha256.Sum256([]byte(incident.Service + "|" + incident.Symptom + "|" + incident.Scenario))
	return hex.EncodeToString(sum[:8])
}

func cloneRun(in *Run) *Run {
	if in == nil {
		return nil
	}
	out := *in
	out.Steps = append([]Step(nil), in.Steps...)
	out.Audit = make([]AuditEvent, len(in.Audit))
	for i, event := range in.Audit {
		out.Audit[i] = event
		if event.Data != nil {
			out.Audit[i].Data = make(map[string]any, len(event.Data))
			for key, value := range event.Data {
				out.Audit[i].Data[key] = value
			}
		}
	}
	return &out
}
