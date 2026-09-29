package knowledge

import "context"

// MemoryDeletionState invalidates retained tool output without exposing another
// owner's private records. Generations only advance on committed deletions.
type MemoryDeletionState struct {
	PrivateGeneration int64 `json:"private_generation"`
	SharedGeneration  int64 `json:"shared_generation"`
}

func (s Service) MemoryDeletions(ctx context.Context, actor string) (MemoryDeletionState, error) {
	var result MemoryDeletionState
	if err := s.knownActor(ctx, actor); err != nil {
		return result, err
	}
	err := s.DB.QueryRow(ctx, `SELECT
	 COALESCE((SELECT generation FROM core.memory_deletion_epochs WHERE owner=$1),0),
	 COALESCE((SELECT generation FROM core.memory_deletion_epochs WHERE owner=''),0)`, actor).
		Scan(&result.PrivateGeneration, &result.SharedGeneration)
	return result, err
}
