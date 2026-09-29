package legacyfood

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// ReadAuthority preserves the event and permission used for an administrator read.
type ReadAuthority struct {
	Event string `json:"event"`
	Scope string `json:"scope"`
}

func (a ReadAuthority) Valid() bool {
	return a.Event != "" && len(a.Event) <= 200 && !strings.ContainsRune(a.Event, 0) &&
		(a.Scope == "review" || a.Scope == "export")
}

func LockReadAuthority(ctx context.Context, tx pgx.Tx, actor string, a ReadAuthority) (bool, error) {
	if !a.Valid() {
		return false, errors.New("invalid food source")
	}
	err := allowed(ctx, tx, actor)
	if err == nil {
		err = adminPermission(ctx, tx, actor, a.Event, a.Scope, true)
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status == http.StatusForbidden {
		return false, nil
	}
	return err == nil, err
}
