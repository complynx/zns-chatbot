package derivedmutation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (s Service) readPassOperation(
	ctx context.Context,
	actor string,
	input PassOperationInput,
) (PassOperationRead, error) {
	r := input
	result := PassOperationSummary{
		Status:       "unknown",
		Continuation: "unavailable",
	}
	if r.Command == nil && r.Assignment == nil && r.Batch == nil {
		return s.readTransportOperation(ctx, actor, input, result)
	}
	actors, source, err := s.passOperationScope(ctx, actor, input)
	if err != nil {
		return PassOperationRead{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return PassOperationRead{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockBatchPrelude(ctx, tx, r.event(), actors, source); err != nil {
		return PassOperationRead{}, err
	}
	receipt, err := s.passOperationReceipt(ctx, tx, actor, r)
	if err != nil {
		return PassOperationRead{}, err
	}
	if r.Batch != nil {
		if err = sameOperationSource(source, receipt.Source); err != nil {
			return PassOperationRead{}, err
		}
	}
	applyReceiptStatus(&result, receipt)
	refs := readsource.Registration(receipt.ReadAuthorities)
	causal, err := operationContext(ctx, tx, actor, r, source, receipt.Transitions, &result)
	if err != nil {
		return PassOperationRead{}, err
	}
	refs, err = readsource.Merge(refs, causal)
	if err != nil {
		return PassOperationRead{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return PassOperationRead{}, err
	}
	return PassOperationRead{Summary: result, ReadAuthorities: refs}, nil
}

func operationContext(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	r PassOperationInput,
	source *readsource.Derivation,
	transitions []passbooking.BookingTransition,
	result *PassOperationSummary,
) ([]readsource.Authority, error) {
	if source != nil && source.Valid() {
		effective := effectiveOperationSource(actor, *source, transitions)
		err := lockSource(ctx, tx, actor, effective)
		if err == nil {
			result.Context = passOperationContext(r)
			result.Continuation = "available"
			return readsource.Capture(actor, effective)
		} else if !operationSourceRetired(err) {
			return nil, err
		}
	}
	return nil, nil
}

func passOperationContext(r PassOperationInput) *PassOperationContext {
	result := &PassOperationContext{Event: r.event()}
	if r.Command != nil {
		result.Target = r.Command.Target
	}
	if r.Assignment != nil {
		result.Target = r.Assignment.Target
	}
	if r.Batch != nil {
		result.Recipients = append([]int64(nil), r.Batch.Recipients...)
		result.RecipientCount = len(r.Batch.Recipients)
	}
	return result
}

// Resolve the original operation scope before taking the transaction locks.
// A persisted batch source is compared again while the batch is locked.
func (s Service) passOperationScope(
	ctx context.Context,
	actor string,
	input PassOperationInput,
) ([]string, *readsource.Derivation, error) {
	r := input
	actors := []string{actor}
	if r.Command != nil && r.Command.Target != "" {
		actors = append(actors, r.Command.Target)
	}
	source := input.Source
	if r.Assignment != nil {
		actors = append(actors, r.Assignment.Target)
	}
	if r.Batch != nil {
		batch, err := s.Registration.ReadRuntimeBatch(ctx, actor, *r.Batch)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, err
		}
		if err == nil {
			actors = batch.Actors()
			source, err = decodeBatchSource(batch.Source)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	return actors, source, nil
}

func sameOperationSource(source *readsource.Derivation, encoded json.RawMessage) error {
	if len(encoded) > 0 {
		previous, encodeErr := json.Marshal(source)
		if encodeErr != nil {
			return encodeErr
		}
		var locked readsource.Derivation
		if err := json.Unmarshal(encoded, &locked); err != nil {
			return err
		}
		current, encodeErr := json.Marshal(locked)
		if encodeErr != nil {
			return encodeErr
		}
		if string(previous) != string(current) {
			return invalidSource()
		}
	}
	return nil
}

func applyReceiptStatus(result *PassOperationSummary, receipt passbooking.OperationReceipt) {
	result.Status, result.Items = receipt.Status, receipt.Items
	for _, item := range receipt.Items {
		if item.Status == passbooking.AdminBatchSucceeded {
			result.Committed++
		}
		if item.Status == passbooking.AdminBatchNotAttempted || item.Status == passbooking.AdminBatchInterrupted {
			result.Pending++
		}
	}
}

func (s Service) passOperationReceipt(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	r PassOperationInput,
) (passbooking.OperationReceipt, error) {
	switch {
	case r.Command != nil:
		return s.Registration.CommandOperationReceipt(ctx, tx, actor, *r.Command)
	case r.Assignment != nil:
		return s.Registration.AssignmentOperationReceipt(ctx, tx, actor, *r.Assignment)
	case r.Batch != nil:
		return s.Registration.BatchOperationReceipt(ctx, tx, actor, *r.Batch)
	default:
		return passbooking.OperationReceipt{}, unavailablePassOperation()
	}
}

func effectiveOperationSource(
	actor string,
	source readsource.Derivation,
	transitions []passbooking.BookingTransition,
) readsource.Derivation {
	effective := source.Clone()
	for i := range effective.Authorities {
		ref := &effective.Authorities[i]
		if ref.Causal == nil {
			advanceBatchLeaf(&ref.Registration, transitions)
			continue
		}
		if ref.Causal.Actor != actor || ref.Causal.Published {
			continue
		}
		for j := range ref.Causal.Authorities {
			advanceBatchLeaf(&ref.Causal.Authorities[j].Registration, transitions)
		}
	}
	return effective
}

func operationSourceRetired(err error) bool {
	var problem *core.ProblemError
	return errors.As(err, &problem) && problem.Status == http.StatusConflict &&
		(problem.Code == "source_stale" || problem.Code == "history_stale" || problem.Code == "history_deleted")
}
