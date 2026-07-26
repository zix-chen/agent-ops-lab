package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zix-chen/agent-ops-lab/internal/agent"
)

func TestCreateRunAndReplay(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := NewServer(agent.NewEngine(nil), logger)
	body, _ := json.Marshal(agent.Incident{
		ID:       "inc-1",
		Service:  "checkout-api",
		Severity: "SEV-1",
		Symptom:  "timeout",
		Scenario: "payment-timeout",
	})

	first := httptest.NewRequest(http.MethodPost, "/api/runs", bytes.NewReader(body))
	first.Header.Set("Idempotency-Key", "same-alert")
	firstRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(firstRecorder, first)
	if firstRecorder.Code != http.StatusCreated {
		t.Fatalf("unexpected status: %d body=%s", firstRecorder.Code, firstRecorder.Body.String())
	}

	replay := httptest.NewRequest(http.MethodPost, "/api/runs", bytes.NewReader(body))
	replay.Header.Set("Idempotency-Key", "same-alert")
	replayRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(replayRecorder, replay)
	if replayRecorder.Code != http.StatusOK {
		t.Fatalf("unexpected replay status: %d", replayRecorder.Code)
	}
	if replayRecorder.Header().Get("X-Idempotent-Replay") != "true" {
		t.Fatalf("expected idempotent replay header")
	}
}

func TestStaticDashboardIsEmbedded(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := NewServer(agent.NewEngine(nil), logger)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected dashboard status: %d", recorder.Code)
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte("Agent Ops Lab")) {
		t.Fatalf("dashboard marker not found")
	}
}
