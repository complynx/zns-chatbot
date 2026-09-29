package agenthost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func (s ScriptStore) LoadModernChoice(ctx context.Context, owner, ref string) (ModernChoiceRecord, error) {
	var draft ModernChoiceRecord
	update, index, sequence, err := ParseModernChoiceRef(ref)
	if err != nil {
		return draft, err
	}
	var record ScriptToolRecord
	var generation *int64
	var redacted bool
	err = s.DB.QueryRow(ctx, `SELECT content->$3::int->'calls'->$4::int,
 (content->$3::int->>'history_generation')::bigint,
 (COALESCE((content->$3::int->>'history_redacted')::boolean,false)
 OR COALESCE((content->$3::int->>'pass_redacted')::boolean,false)
 OR COALESCE((content->$3::int->>'memory_redacted')::boolean,false))
 FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='script_runs' AND created_at>clock_timestamp()-interval '24 hours'`, owner, update, index, sequence).Scan(&record, &generation, &redacted)
	if err != nil {
		return draft, errors.New("choice receipt unavailable")
	}
	if generation == nil || redacted || record.ModernChoice == nil || record.ModernChoice.HistoryGeneration == nil ||
		*generation != *record.ModernChoice.HistoryGeneration {
		return draft, s.StaleError
	}
	if record.ModernChoice == nil || record.ModernChoice.Ref != ref || record.ModernChoice.Child != "" ||
		record.Outcome.Error != "" ||
		!choiceResultPresent(record.Outcome.Result) {
		return draft, errors.New("choice revision unavailable or consumed")
	}
	return *record.ModernChoice, nil
}

// ClaimModernChoice claims the successful parent in the admission transaction.
func (s ScriptStore) ClaimModernChoice(
	ctx context.Context,
	tx pgx.Tx,
	owner string,
	update int64,
	records []ScriptRecord,
	ref, child string,
	generation int64,
) error {
	parentUpdate, index, sequence, err := ParseModernChoiceRef(ref)
	if err != nil {
		return err
	}
	if parentUpdate == update {
		if index >= len(records) || sequence >= len(records[index].Calls) {
			return errors.New("choice receipt missing")
		}
		parent := &records[index]
		record := &parent.Calls[sequence]
		if scriptRetired(*parent) || parent.HistoryGeneration != generation || record.ModernChoice == nil ||
			record.ModernChoice.HistoryGeneration == nil || *record.ModernChoice.HistoryGeneration != generation {
			return s.StaleError
		}
		if record.ModernChoice == nil || record.ModernChoice.Ref != ref || record.ModernChoice.Child != "" ||
			record.Outcome.Error != "" ||
			!choiceResultPresent(record.Outcome.Result) {
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
 AND content->$5::int->'calls'->$6::int->'outcome'->'result' <> 'null'::jsonb
 AND (content->$5::int->>'history_generation')::bigint=$8
 AND COALESCE((content->$5::int->>'history_redacted')::boolean,false)=false
 AND COALESCE((content->$5::int->>'pass_redacted')::boolean,false)=false
 AND COALESCE((content->$5::int->>'memory_redacted')::boolean,false)=false
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

func choiceResultPresent(result json.RawMessage) bool {
	trimmed := bytes.TrimSpace(result)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func (s ScriptStore) admitModernChoice(
	ctx context.Context,
	tx pgx.Tx,
	owner string,
	update int64,
	index, sequence int,
	records []ScriptRecord,
	call *ScriptToolRecord,
) error {
	ref := fmt.Sprintf("%d.%d.%d", update, index, sequence)
	if call.ModernChoice != nil {
		if call.ModernChoice.HistoryGeneration == nil ||
			*call.ModernChoice.HistoryGeneration != records[index].HistoryGeneration {
			return s.StaleError
		}
		call.ModernChoice.Ref = ref
		if call.ModernChoice.Parent != "" {
			return s.ClaimModernChoice(
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
			return s.StaleError
		}
		return s.ClaimModernChoice(
			ctx,
			tx,
			owner,
			update,
			records,
			call.ChoiceUse,
			ref,
			records[index].HistoryGeneration,
		)
	}
	return nil
}
func ParseModernChoiceRef(ref string) (int64, int, int, error) {
	var update int64
	var run, call int
	if _, err := fmt.Sscanf(
		ref,
		"%d.%d.%d",
		&update,
		&run,
		&call,
	); err != nil || update <= 0 || run < 0 || run >= agent.MaxScriptRuns || call < 0 || call >= MaxScriptCalls ||
		ref != fmt.Sprintf("%d.%d.%d", update, run, call) {
		return 0, 0, 0, errors.New("invalid choice reference")
	}
	return update, run, call, nil
}

// ClearUncommittedModernOrderRead requires a durable page receipt before mutation.
func ClearUncommittedModernOrderRead(input *agent.Input, name string, committed bool) {
	if !committed && (name == "orders.inspect" || name == "orders.review.read") {
		input.ModernOrder = nil
		input.ModernOrderCursor = ""
	}
}
