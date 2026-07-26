package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zix-chen/agent-ops-lab/internal/agent"
	"github.com/zix-chen/agent-ops-lab/internal/api"
	"github.com/zix-chen/agent-ops-lab/internal/llm"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	var enricher agent.Enricher
	if key := os.Getenv("LLM_API_KEY"); key != "" {
		baseURL := envOrDefault("LLM_BASE_URL", "https://api.openai.com/v1")
		model := envOrDefault("LLM_MODEL", "gpt-4.1-mini")
		enricher = llm.NewOpenAICompatible(baseURL, key, model)
		logger.Info("optional LLM enrichment enabled", "model", model)
	} else {
		logger.Info("running in deterministic demo mode")
	}

	engine := agent.NewEngine(enricher)
	handler := api.NewServer(engine, logger).Handler()
	server := api.NewHTTPServer(api.Address(os.Getenv("PORT")), handler)

	go func() {
		logger.Info("agent ops lab started", "address", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("shutdown failed", "error", err)
	}
	logger.Info("server stopped")
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
