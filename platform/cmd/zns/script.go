package main

import (
	"context"
	"encoding/json"

	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type observedScripts struct {
	next    *scriptclient.Client
	runtime *observability.Runtime
}

func (s observedScripts) Evaluate(ctx context.Context, input scriptclient.Request) (json.RawMessage, error) {
	ctx, finish := s.runtime.Start(ctx, "js.run")
	result, err := s.next.Evaluate(ctx, input)
	finish(err)
	return result, err
}

func (s observedScripts) Execute(
	ctx context.Context,
	input scriptclient.Request,
	tools []scriptclient.Tool,
	callback scriptclient.Callback,
) (json.RawMessage, error) {
	ctx, finish := s.runtime.Start(ctx, "js.run")
	result, err := s.next.Execute(ctx, input, tools, callback)
	finish(err)
	return result, err
}

// The application never imports a JS VM. Only an explicitly configured Unix
// socket can supply scripts; the operator must deploy the isolated helper.
func configureBotScripts(b *bot.Bot, cfg config.Config, runtime *observability.Runtime) (func(), error) {
	if !cfg.Script.Enabled {
		return func() {}, nil
	}
	client, err := scriptclient.New(cfg.Script.Socket)
	if err != nil {
		return nil, err
	}
	b.Scripts = observedScripts{next: client, runtime: runtime}
	return client.Close, nil
}
