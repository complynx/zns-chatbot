package knowledge

import (
	"context"

	"github.com/jackc/pgx/v5"

	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
)

// MemoryDeletionState invalidates retained tool output without exposing another
// owner's private records. Generations only advance on committed deletions.
type MemoryDeletionState struct {
	PrivateGeneration int64 `json:"private_generation"`
	SharedGeneration  int64 `json:"shared_generation"`
}

const memoryDeletionStateQuery = `SELECT
 COALESCE((SELECT generation FROM core.memory_deletion_epochs WHERE owner=$1),0),
 COALESCE((SELECT generation FROM core.memory_deletion_epochs WHERE owner=''),0)`

func (s Service) MemoryDeletions(ctx context.Context, actor string) (MemoryDeletionState, error) {
	var result MemoryDeletionState
	if err := s.knownActor(ctx, actor); err != nil {
		return result, err
	}
	err := s.DB.QueryRow(ctx, memoryDeletionStateQuery, actor).
		Scan(&result.PrivateGeneration, &result.SharedGeneration)
	return result, err
}

// LockMemoryDeletions keeps both retirement generations stable through the
// caller's transaction. Take these domain gates before host advisory/row locks.
func LockMemoryDeletions(ctx context.Context, tx pgx.Tx, actor string) (MemoryDeletionState, error) {
	var result MemoryDeletionState
	if err := lockActor(ctx, tx, actor); err != nil {
		return result, err
	}
	if err := knowledgeauthority.LockSharedGate(ctx, tx); err != nil {
		return result, err
	}
	err := tx.QueryRow(ctx, memoryDeletionStateQuery, actor).
		Scan(&result.PrivateGeneration, &result.SharedGeneration)
	return result, err
}
