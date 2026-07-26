package agent

import "time"

type RunStatus string

const (
	StatusResolved RunStatus = "resolved"
	StatusApproval RunStatus = "approval_required"
	StatusBlocked  RunStatus = "blocked"
	StatusDegraded RunStatus = "degraded"
)

type Incident struct {
	ID       string `json:"id"`
	Service  string `json:"service"`
	Severity string `json:"severity"`
	Symptom  string `json:"symptom"`
	Scenario string `json:"scenario"`
}

type Step struct {
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Attempts   int       `json:"attempts"`
	Detail     string    `json:"detail"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
}

type AuditEvent struct {
	At      time.Time      `json:"at"`
	Actor   string         `json:"actor"`
	Action  string         `json:"action"`
	Outcome string         `json:"outcome"`
	Data    map[string]any `json:"data,omitempty"`
}

type Run struct {
	ID             string       `json:"id"`
	Incident       Incident     `json:"incident"`
	Status         RunStatus    `json:"status"`
	Diagnosis      string       `json:"diagnosis"`
	Recommendation string       `json:"recommendation"`
	Risk           string       `json:"risk"`
	Confidence     float64      `json:"confidence"`
	Steps          []Step       `json:"steps"`
	Audit          []AuditEvent `json:"audit"`
	CreatedAt      time.Time    `json:"createdAt"`
	FinishedAt     time.Time    `json:"finishedAt"`
}

type Scenario struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Service     string `json:"service"`
	Severity    string `json:"severity"`
	Symptom     string `json:"symptom"`
	Description string `json:"description"`
	Signal      string `json:"signal"`
}

type Metrics struct {
	TotalRuns       int     `json:"totalRuns"`
	Resolved        int     `json:"resolved"`
	ApprovalPending int     `json:"approvalPending"`
	Blocked         int     `json:"blocked"`
	Degraded        int     `json:"degraded"`
	Retried         int     `json:"retried"`
	Deduplicated    int     `json:"deduplicated"`
	AvgLatencyMs    float64 `json:"avgLatencyMs"`
	CircuitOpen     bool    `json:"circuitOpen"`
}

type EvaluationCase struct {
	Name           string    `json:"name"`
	Scenario       string    `json:"scenario"`
	ExpectedStatus RunStatus `json:"expectedStatus"`
	ActualStatus   RunStatus `json:"actualStatus"`
	Passed         bool      `json:"passed"`
	Guardrail      string    `json:"guardrail"`
}

type EvaluationReport struct {
	ID        string           `json:"id"`
	Passed    int              `json:"passed"`
	Total     int              `json:"total"`
	Score     float64          `json:"score"`
	Duration  int64            `json:"durationMs"`
	Cases     []EvaluationCase `json:"cases"`
	CreatedAt time.Time        `json:"createdAt"`
}
