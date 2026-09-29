package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

// Match the clients' existing defaults when supplying instrumented transports.
const (
	apiClientTimeout      = 10 * time.Second
	telegramClientTimeout = 15 * time.Second
	mediaClientTimeout    = 20 * time.Second
	openAIClientTimeout   = 30 * time.Second
	remoteClientTimeout   = 40 * time.Second
)

func configuredLogger(writer io.Writer, cfg config.Config) *slog.Logger {
	level, _ := cfg.Log.SlogLevel() // Config.Load validates the level before startup.
	return observability.NewLogger(writer, observability.LogConfig{Level: level, Secrets: []string{
		cfg.Database.URL.Value(), cfg.Auth.SigningKey.Value(), cfg.Telegram.Token.Value(),
		cfg.Model.OpenAIKey.Value(), cfg.Media.Secret.Value(), cfg.Sticker.Worker.Secret.Value(),
		cfg.Auth.Zitadel.BotClientSecret.Value(), cfg.Auth.Zitadel.APIClientSecret.Value(),
		cfg.Auth.Zitadel.ActorClientSecret.Value(),
	}})
}

func telemetryHandler(runtime *observability.Runtime, handler http.Handler) http.Handler {
	metrics := runtime.Handler()
	application := runtime.HTTPHandler("server", handler)
	// Leave path validation and redirects to the application's public boundary.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			metrics.ServeHTTP(w, r)
			return
		}
		application.ServeHTTP(w, r)
	})
}

func telemetryClient(runtime *observability.Runtime, operation string, timeout time.Duration) *http.Client {
	return &http.Client{Transport: runtime.HTTPClient(operation, http.DefaultTransport), Timeout: timeout}
}

func flushTelemetry(ctx context.Context, runtime *observability.Runtime, timeout time.Duration) error {
	flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	return runtime.Shutdown(flush)
}

type observedModel struct {
	next    agent.Model
	runtime *observability.Runtime
}

func (m observedModel) Plan(ctx context.Context, input agent.Input) (agent.Plan, error) {
	ctx, finish := m.runtime.Start(ctx, "model.plan")
	plan, err := m.next.Plan(ctx, input)
	finish(err)
	return plan, err
}

func (m observedModel) AssessKnowledge(
	ctx context.Context,
	input agent.KnowledgeAssessmentInput,
) (agent.KnowledgeAssessment, error) {
	ctx, finish := m.runtime.Start(ctx, "model.knowledge_assessment")
	classifier, ok := m.next.(agent.KnowledgeClassifier)
	if !ok {
		err := errors.New("knowledge classifier unavailable")
		finish(err)
		return agent.KnowledgeAssessment{}, err
	}
	verdict, err := classifier.AssessKnowledge(ctx, input)
	finish(err)
	return verdict, err
}

func (m observedModel) SummarizeHistory(ctx context.Context, input agent.HistorySummaryInput) (string, error) {
	ctx, finish := m.runtime.Start(ctx, "model.history_summary")
	summarizer, ok := m.next.(agent.HistorySummarizer)
	if !ok {
		err := errors.New("history summarizer unavailable")
		finish(err)
		return "", err
	}
	text, err := summarizer.SummarizeHistory(ctx, input)
	finish(err)
	return text, err
}

func (m observedModel) InformalName(ctx context.Context, fields map[string]any) (string, error) {
	ctx, finish := m.runtime.Start(ctx, "model.broadcast_name")
	namer, ok := m.next.(agent.BroadcastNamer)
	if !ok {
		err := errors.New("broadcast name unavailable")
		finish(err)
		return "", err
	}
	name, err := namer.InformalName(ctx, fields)
	finish(err)
	return name, err
}
