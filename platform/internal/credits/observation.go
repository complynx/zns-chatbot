package credits

import (
	"context"
	"errors"
	"time"
)

const UsageObservationLimit = 1024
const UsageObservationLookback = 24 * time.Hour
const UsageObservationExactMaximum int64 = 1<<53 - 1
const observationSettledState = "settled"

// UsageTokenObservation sums known counts only. Missing counters are unknown,
// never zero. Capped means the uncapped sum exceeds exact Prometheus precision;
// Sum then holds the largest exact float integer, not the original total.
type UsageTokenObservation struct {
	Sum             int64
	KnownReceipts   int64
	UnknownReceipts int64
	Capped          bool
}

// UsageObservation is a bounded recent sample, not a whole-window total. States
// are reserved/dispatched/settled/not_sent; bases reported/estimated/unknown/
// known_free. Tokens are input/cached_input/cache_write/output/reasoning/
// audio_input/text_input. Cached and other subsets must not be added to input.
type UsageObservation struct {
	States    [4]int64
	Bases     [4]int64
	Tokens    [7]UsageTokenObservation
	Truncated bool
}

// ObserveUsage reads at most limit+1 recent rows using the global time index.
// Settled does not imply provider success; failed calls can have known usage.
// It does not read IDs, subjects, prompts, model names or receipt identifiers.
func (s Service) ObserveUsage(ctx context.Context) (UsageObservation, error) {
	if s.DB == nil {
		return UsageObservation{}, errors.New("model usage observation configuration missing")
	}
	const timeout = 2 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	rows, err := s.DB.Query(ctx, `SELECT state,
 COALESCE(usage->'usage'->>'basis','unknown'),
 CASE WHEN state='settled' THEN (usage->'usage'->>'input_tokens')::bigint END,
 CASE WHEN state='settled' THEN (usage->'usage'->>'cached_input_tokens')::bigint END,
 CASE WHEN state='settled' THEN (usage->'usage'->>'cache_write_tokens')::bigint END,
 CASE WHEN state='settled' THEN (usage->'usage'->>'output_tokens')::bigint END,
 CASE WHEN state='settled' THEN (usage->'usage'->>'reasoning_tokens')::bigint END,
 CASE WHEN state='settled' THEN (usage->'usage'->>'audio_input_tokens')::bigint END,
 CASE WHEN state='settled' THEN (usage->'usage'->>'text_input_tokens')::bigint END
 FROM credits.attempts
 WHERE created_at>=statement_timestamp()-interval '24 hours' AND created_at<=statement_timestamp()
 ORDER BY created_at DESC,id DESC LIMIT $1`, UsageObservationLimit+1)
	if err != nil {
		return UsageObservation{}, err
	}
	defer rows.Close()
	var result UsageObservation
	count := 0
	for rows.Next() {
		if count == UsageObservationLimit {
			result.Truncated = true
			break
		}
		var state string
		var usage Usage
		if err = rows.Scan(
			&state,
			&usage.Basis,
			&usage.Input,
			&usage.Cached,
			&usage.CacheWrite,
			&usage.Output,
			&usage.Reasoning,
			&usage.AudioInput,
			&usage.TextInput,
		); err != nil {
			return UsageObservation{}, err
		}
		if err = result.add(state, usage); err != nil {
			return UsageObservation{}, err
		}
		count++
	}
	if err = rows.Err(); err != nil {
		return UsageObservation{}, err
	}
	return result, nil
}

func (o *UsageObservation) add(state string, usage Usage) error {
	index := -1
	switch state {
	case "reserved":
		index = 0
	case "dispatched":
		index = 1
	case observationSettledState:
		index = 2
	case "not_sent":
		index = 3
	}
	if index < 0 {
		return ErrInvalid
	}
	if state != observationSettledState {
		o.States[index]++
		return nil
	}
	if usage.Validate() != nil {
		return ErrInvalid
	}
	basis := -1
	switch usage.Basis {
	case basisReported:
		basis = 0
	case basisEstimated:
		basis = 1
	case basisUnknown:
		basis = 2
	case basisFree:
		basis = 3
	}
	if basis < 0 {
		return ErrInvalid
	}
	o.States[index]++
	o.Bases[basis]++
	for i, value := range []*int64{usage.Input, usage.Cached, usage.CacheWrite, usage.Output, usage.Reasoning, usage.AudioInput, usage.TextInput} {
		item := &o.Tokens[i]
		if value == nil {
			item.UnknownReceipts++
			continue
		}
		item.KnownReceipts++
		if *value > UsageObservationExactMaximum-item.Sum {
			item.Sum = UsageObservationExactMaximum
			item.Capped = true
		} else {
			item.Sum += *value
		}
	}
	return nil
}
