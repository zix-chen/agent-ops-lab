package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/zix-chen/agent-ops-lab/internal/agent"
	"github.com/zix-chen/agent-ops-lab/webui"
)

type Server struct {
	engine *agent.Engine
	logger *slog.Logger
	mux    *http.ServeMux
}

func NewServer(engine *agent.Engine, logger *slog.Logger) *Server {
	server := &Server{
		engine: engine,
		logger: logger,
		mux:    http.NewServeMux(),
	}
	server.routes()
	return server
}

func (s *Server) Handler() http.Handler {
	return securityHeaders(s.accessLog(s.mux))
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("GET /api/scenarios", s.scenarios)
	s.mux.HandleFunc("GET /api/runs", s.runs)
	s.mux.HandleFunc("POST /api/runs", s.createRun)
	s.mux.HandleFunc("GET /api/metrics", s.metrics)
	s.mux.HandleFunc("POST /api/evaluations/run", s.evaluate)
	s.mux.HandleFunc("POST /api/reset", s.reset)
	s.mux.Handle("/", webui.Handler())
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"timestamp": time.Now().UTC(),
	})
}

func (s *Server) scenarios(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, agent.Scenarios())
}

func (s *Server) runs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.engine.ListRuns())
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	var incident agent.Incident
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&incident); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	run, duplicate, err := s.engine.Run(r.Context(), incident, key)
	if err != nil {
		if errors.Is(err, agent.ErrInvalidIncident) {
			writeError(w, http.StatusUnprocessableEntity, "invalid_incident", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "run_failed", "the run could not be completed")
		return
	}
	if duplicate {
		w.Header().Set("X-Idempotent-Replay", "true")
	}
	status := http.StatusCreated
	if duplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{
		"duplicate": duplicate,
		"run":       run,
	})
}

func (s *Server) metrics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.engine.Metrics())
}

func (s *Server) evaluate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, agent.RunEvaluation(r.Context()))
}

func (s *Server) reset(w http.ResponseWriter, _ *http.Request) {
	s.engine.Reset()
	writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"duration", time.Since(start).String(),
			)
		}
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("encode response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func NewHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelError),
	}
}

func Address(port string) string {
	if strings.TrimSpace(port) == "" {
		port = "8080"
	}
	return fmt.Sprintf(":%s", port)
}
