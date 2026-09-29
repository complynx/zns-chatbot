// Package derivedmutation owns source fences around typed application effects.
package derivedmutation

import (
	"context"
	"net/http"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/workflow"

	"github.com/complynx/zns-chatbot/platform/internal/account"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/conversation/fence"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type Service struct {
	Account       account.Service
	DB            *pgxpool.Pool
	Orders        orders.Service
	Workflow      workflow.Service
	ModelSettings modelsettings.Service
	Credits       credits.Service
	Food          legacyfood.Service
	Massage       massage.Service
	Registration  passbooking.Service
	Profile       passes.Service
}

const agentOrigin = "agent"

func invalidSource() error {
	return &core.ProblemError{Status: http.StatusBadRequest, Code: "invalid_derivation"}
}

// lockSource is called only after target authorization and receipt lookup. A
// committed effect stays replayable; a new effect retains these locks to commit.
func lockSource(ctx context.Context, tx pgx.Tx, actor string, source readsource.Derivation) error {
	valid, err := readsource.Lock(ctx, tx, actor, source.Authorities)
	if err != nil {
		return err
	}
	if slices.Contains(valid, false) {
		return &core.ProblemError{Status: http.StatusConflict, Code: "source_stale"}
	}
	return fence.LockGeneration(ctx, tx, actor, source.Generation)
}
