package bot

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func (b *Bot) resolveModernChoice(
	ctx context.Context,
	owner, name string,
	args *modernOrderArguments,
	record *scriptToolRecord,
) error {
	if args.ChoiceRef == "" {
		return nil
	}
	draft, _, err := b.currentModernChoice(ctx, owner, args.ChoiceRef)
	if err != nil {
		return err
	}
	if args.Event != "" && args.Event != draft.Event {
		return errors.New("choice event mismatch")
	}
	if name == modernOrdersUpdate {
		if (args.Name != actionCreateOrder && args.Name != modernOrderEdit) || args.OrderID != draft.OrderID {
			return errors.New("choice target mismatch")
		}
		if record == nil {
			return errors.New("choice command binding missing")
		}
		record.ChoiceUse = args.ChoiceRef
		record.ChoiceCatalog = draft.Catalog
		record.ChoiceGeneration = draft.HistoryGeneration
	}
	choice := choiceInput(draft.Choice)
	args.Event, args.Choice = draft.Event, &choice
	return nil
}

func (b *Bot) loadModernChoice(ctx context.Context, owner, ref string) (modernChoiceRecord, error) {
	var draft modernChoiceRecord
	update, index, sequence, err := parseModernChoiceRef(ref)
	if err != nil {
		return draft, err
	}
	var record scriptToolRecord
	var generation *int64
	var redacted bool
	err = b.DB.QueryRow(ctx, `SELECT content->$3::int->'calls'->$4::int,
 (content->$3::int->>'history_generation')::bigint,COALESCE((content->$3::int->>'history_redacted')::boolean,false)
 FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='script_runs' AND created_at>clock_timestamp()-interval '24 hours'`, owner, update, index, sequence).Scan(&record, &generation, &redacted)
	if err != nil {
		return draft, errors.New("choice receipt unavailable")
	}
	if generation == nil || redacted || record.ModernChoice == nil || record.ModernChoice.HistoryGeneration == nil ||
		*generation != *record.ModernChoice.HistoryGeneration {
		return draft, errScriptReadStale
	}
	if record.ModernChoice == nil || record.ModernChoice.Ref != ref || record.ModernChoice.Child != "" ||
		record.Outcome.Error != "" ||
		len(record.Outcome.Result) == 0 {
		return draft, errors.New("choice revision unavailable or consumed")
	}
	return *record.ModernChoice, nil
}

func (b *Bot) currentModernChoice(ctx context.Context, owner, ref string) (modernChoiceRecord, orders.Event, error) {
	draft, err := b.loadModernChoice(ctx, owner, ref)
	if err != nil {
		return draft, orders.Event{}, err
	}
	event, err := b.API.OrderEvent(ctx, owner, draft.Event)
	if err != nil {
		return draft, event, err
	}
	if modernCatalogFingerprint(event) != draft.Catalog {
		return draft, event, errScriptReadStale
	}
	if draft.OrderID != "" {
		order, getErr := b.API.Order(ctx, owner, draft.Event, draft.OrderID)
		if getErr != nil {
			return draft, event, getErr
		}
		fingerprint, hashErr := modernOrderFingerprint(order)
		if hashErr != nil {
			return draft, event, hashErr
		}
		if fingerprint != draft.Snapshot {
			return draft, event, errScriptReadStale
		}
	}
	return draft, event, b.validateModernChoiceGeneration(ctx, owner, draft)
}

func (b *Bot) validateModernChoiceGeneration(ctx context.Context, owner string, draft modernChoiceRecord) error {
	if draft.HistoryGeneration == nil {
		return errScriptReadStale
	}
	return b.API.checkHistoryGeneration(ctx, owner, *draft.HistoryGeneration)
}

// Claim the exact successful revision in the same transaction as admission.
// References address one bounded script receipt directly; no chain/history scan.
func claimModernChoice(
	ctx context.Context,
	tx pgx.Tx,
	owner string,
	update int64,
	records []scriptRecord,
	ref, child string,
	generation int64,
) error {
	parentUpdate, index, sequence, err := parseModernChoiceRef(ref)
	if err != nil {
		return err
	}
	if parentUpdate == update {
		if index >= len(records) || sequence >= len(records[index].Calls) {
			return errors.New("choice receipt missing")
		}
		parent := &records[index]
		record := &parent.Calls[sequence]
		if parent.HistoryRedacted || parent.HistoryGeneration != generation || record.ModernChoice == nil ||
			record.ModernChoice.HistoryGeneration == nil || *record.ModernChoice.HistoryGeneration != generation {
			return errScriptReadStale
		}
		if record.ModernChoice == nil || record.ModernChoice.Ref != ref || record.ModernChoice.Child != "" ||
			record.Outcome.Error != "" ||
			len(record.Outcome.Result) == 0 {
			return errors.New("choice revision consumed")
		}
		record.ModernChoice.Child = child
		return nil
	}
	path := []string{strconv.Itoa(index), "calls", strconv.Itoa(sequence), "modern_choice", "child"}
	result, err := tx.Exec(
		ctx,
		`UPDATE bot.interactions SET content=jsonb_set(content,$3::text[],to_jsonb($4::text),true)
 WHERE owner=$1 AND update_id=$2 AND kind='script_runs' AND created_at>clock_timestamp()-interval '24 hours'
 AND content->$5::int->'calls'->$6::int->'modern_choice'->>'ref'=$7
 AND COALESCE(content->$5::int->'calls'->$6::int->'modern_choice'->>'child','')=''
 AND COALESCE(content->$5::int->'calls'->$6::int->'outcome'->>'error','')=''
 AND content->$5::int->'calls'->$6::int->'outcome' ? 'result'
 AND (content->$5::int->>'history_generation')::bigint=$8
 AND COALESCE((content->$5::int->>'history_redacted')::boolean,false)=false
 AND (content->$5::int->'calls'->$6::int->'modern_choice'->>'history_generation')::bigint=$8`,
		owner,
		parentUpdate,
		path,
		child,
		index,
		sequence,
		ref, generation,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errors.New("choice revision consumed")
	}
	return nil
}

func admitModernChoice(
	ctx context.Context,
	tx pgx.Tx,
	owner string,
	update int64,
	index, sequence int,
	records []scriptRecord,
	call *scriptToolRecord,
) error {
	ref := fmt.Sprintf("%d.%d.%d", update, index, sequence)
	if call.ModernChoice != nil {
		if call.ModernChoice.HistoryGeneration == nil ||
			*call.ModernChoice.HistoryGeneration != records[index].HistoryGeneration {
			return errScriptReadStale
		}
		call.ModernChoice.Ref = ref
		if call.ModernChoice.Parent != "" {
			return claimModernChoice(
				ctx,
				tx,
				owner,
				update,
				records,
				call.ModernChoice.Parent,
				ref,
				records[index].HistoryGeneration,
			)
		}
	}
	if call.ChoiceUse != "" {
		if call.ChoiceGeneration == nil || *call.ChoiceGeneration != records[index].HistoryGeneration {
			return errScriptReadStale
		}
		return claimModernChoice(ctx, tx, owner, update, records, call.ChoiceUse, ref, records[index].HistoryGeneration)
	}
	return nil
}
