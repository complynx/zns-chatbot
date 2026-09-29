package knowledge

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func lockKnowledgeCommand(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	c Command,
	refs []readsource.Authority,
	derived bool,
) error {
	if derived {
		if err := readsource.LockEvents(ctx, tx, refs); err != nil {
			return err
		}
		if err := readsource.LockActors(ctx, tx, []string{actor}, refs); err != nil {
			return err
		}
	}

	if err := lockActor(ctx, tx, actor); err != nil {
		return err
	}
	if c.Name == RemoveFact || derivedSharedMemory(readsource.Derivation{Authorities: refs}) {
		// Match shared-memory provenance locks before the target scope or fact.
		if err := lockScope(ctx, tx, ""); err != nil {
			return err
		}
	}
	if c.Name != MemoSet && c.Name != MemoDelete && c.Name != DocumentSet && c.Name != DocumentDelete {
		if err := lockScope(ctx, tx, c.Event); err != nil {
			return err
		}
	}
	if err := authorize(ctx, tx, actor, c); err != nil {
		return err
	}
	return nil
}

func knowledgeOperationBytes(c Command, keys []string, source *readsource.Derivation) ([]byte, error) {
	encoded, err := memoryCommandBytes(c, keys)
	if err != nil || source == nil {
		return encoded, err
	}
	return json.Marshal(struct {
		Command json.RawMessage       `json:"command"`
		Source  readsource.Derivation `json:"source"`
	}{encoded, *source})
}
