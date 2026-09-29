package agenthost

import (
	"context"
	"errors"
)

// ModernChoiceMetadata binds a quote to an owner-scoped retained receipt. The
// complete choice and current domain authority must still be loaded at execution.
type ModernChoiceMetadata struct {
	Event             string
	Catalog           string
	OrderID           string
	Snapshot          string
	HistoryGeneration int64
}

func (s ScriptStore) LoadModernChoiceMetadata(ctx context.Context, owner, ref string) (ModernChoiceMetadata, error) {
	var result ModernChoiceMetadata
	update, index, sequence, err := ParseModernChoiceRef(ref)
	if err != nil {
		return result, err
	}
	var event, catalog, orderID, snapshot *string
	var generation, choiceGeneration *int64
	var redacted, valid bool
	err = s.DB.QueryRow(ctx, `SELECT
 CASE WHEN octet_length(call_record->'modern_choice'->>'event') BETWEEN 1 AND 128 THEN call_record->'modern_choice'->>'event' END,
 CASE WHEN octet_length(call_record->'modern_choice'->>'catalog')=64 THEN call_record->'modern_choice'->>'catalog' END,
 CASE WHEN octet_length(COALESCE(call_record->'modern_choice'->>'order_id',''))<=128 THEN COALESCE(call_record->'modern_choice'->>'order_id','') END,
 CASE WHEN octet_length(COALESCE(call_record->'modern_choice'->>'snapshot','')) IN (0,64) THEN COALESCE(call_record->'modern_choice'->>'snapshot','') END,
 (run_record->>'history_generation')::bigint,
 (call_record->'modern_choice'->>'history_generation')::bigint,
 (COALESCE((run_record->>'history_redacted')::boolean,false) OR COALESCE((run_record->>'pass_redacted')::boolean,false) OR COALESCE((run_record->>'memory_redacted')::boolean,false)),
 COALESCE(call_record->'modern_choice'->>'ref'=$5
 AND COALESCE(call_record->'modern_choice'->>'child','')=''
 AND COALESCE(call_record->'outcome'->>'error','')=''
 AND (call_record->'outcome' ? 'result') AND call_record->'outcome'->'result' <> 'null'::jsonb,false)
 FROM (SELECT content->$3::int AS run_record,content->$3::int->'calls'->$4::int AS call_record
 FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='script_runs'
 AND created_at>clock_timestamp()-interval '24 hours') receipt`, owner, update, index, sequence, ref).Scan(&event, &catalog, &orderID, &snapshot, &generation, &choiceGeneration, &redacted, &valid)
	if err != nil {
		return result, errors.New("choice receipt unavailable")
	}
	if generation == nil || choiceGeneration == nil || redacted || *generation != *choiceGeneration {
		return result, s.StaleError
	}
	if !valid || event == nil || catalog == nil || orderID == nil || snapshot == nil ||
		(*orderID != "" && len(*snapshot) != 64) {
		return result, errors.New("choice revision unavailable or consumed")
	}
	result.Event = *event
	result.Catalog, result.OrderID, result.Snapshot = *catalog, *orderID, *snapshot
	result.HistoryGeneration = *choiceGeneration
	return result, nil
}
