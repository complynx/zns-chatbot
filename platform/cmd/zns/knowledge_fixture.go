package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func knowledgeFixtureConfig() (sandbox.KnowledgeFixture, bool, error) {
	return parseKnowledgeFixture(os.Getenv("KNOWLEDGE_FIXTURE_ACTION"), os.Getenv("KNOWLEDGE_FIXTURE_STAND"),
		os.Getenv("KNOWLEDGE_FIXTURE_SCOPE"), os.Getenv("KNOWLEDGE_FIXTURE_PERMISSION"))
}

func parseKnowledgeFixture(action, stand, scope, permission string) (sandbox.KnowledgeFixture, bool, error) {
	f := sandbox.KnowledgeFixture{Stand: stand, Action: action, Scope: scope, Permission: permission}
	if f.Stand == "" && f.Action == "" && f.Scope == "" && f.Permission == "" {
		return f, false, nil
	}
	// Empty scope is general knowledge; all other binding fields are required.
	if f.Stand == "" || f.Action == "" || f.Permission == "" {
		return f, true, errors.New("knowledge fixture action, stand and permission are required")
	}
	return f, true, f.Validate()
}

func runKnowledgeFixture(ctx context.Context, db *pgxpool.Pool, f sandbox.KnowledgeFixture) error {
	state, err := sandbox.ApplyKnowledgeFixture(ctx, db, f)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(state)
}
