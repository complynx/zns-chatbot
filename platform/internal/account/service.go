// Package account owns user preferences and trusted sender metadata.
package account

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type Service struct{ DB *pgxpool.Pool }

func fail(status int, code string) error { return &core.ProblemError{Status: status, Code: code} }
