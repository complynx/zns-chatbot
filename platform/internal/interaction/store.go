package interaction

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction/dbgen"
)

// Store owns durable application plans and their terminal privacy transitions.
// Domain command receipts remain in their domain's idempotency store.
type Store struct{ DB *pgxpool.Pool }

type Notice struct {
	Content       []byte
	Native        bool
	UpdateID      int64
	System        i18n.ID
	SourceRevoked bool
}

func (s Store) ReplyOrigin(ctx context.Context, owner string, updateID int64) (ReplyOrigin, error) {
	origin, err := dbgen.New(s.DB).ReplyOrigin(ctx, dbgen.ReplyOriginParams{Owner: owner, UpdateID: updateID})
	return ReplyOrigin(origin), core.DatabaseOperationError(err)
}

func (s Store) LatestNotice(ctx context.Context, owner string) (Notice, error) {
	row, err := dbgen.New(s.DB).LatestNotice(ctx, owner)
	if err != nil {
		return Notice{}, core.DatabaseOperationError(err)
	}
	notice := Notice{Content: row.Content, Native: row.NativeMarkdown, UpdateID: row.UpdateID}
	if row.Payload != nil {
		plan, decodeErr := decodePlan(
			dbgen.InteractionSavedTurn{
				Payload:           row.Payload,
				Kind:              row.Kind,
				State:             row.State,
				Reason:            row.Reason,
				HistoryGeneration: row.HistoryGeneration,
			},
		)
		if decodeErr != nil {
			return Notice{}, decodeErr
		}
		notice.System = plan.SystemNotice
		notice.SourceRevoked = plan.TerminalReason == SourceRevoked && !row.Trusted
	}
	return notice, nil
}

func (s Store) ConsumedVoice(ctx context.Context, owner, mediaID string, updateID int64) (bool, error) {
	row, err := dbgen.New(s.DB).
		ConsumedVoice(ctx, dbgen.ConsumedVoiceParams{Owner: owner, ID: mediaID, UpdateID: updateID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = decodePlan(
		dbgen.InteractionSavedTurn{
			Payload:           row.Payload,
			Kind:              row.Kind,
			State:             row.State,
			Reason:            row.Reason,
			HistoryGeneration: row.HistoryGeneration,
		},
	)
	if err != nil {
		return false, err
	}
	return row.Consumed, nil
}

// Load only returns the plan belonging to this owner and update.
func (s Store) Load(ctx context.Context, owner string, updateID int64) (SavedPlan, error) {
	if owner == "" {
		return SavedPlan{}, errors.New("missing turn owner")
	}
	row, err := dbgen.New(s.DB).LoadTurn(ctx, dbgen.LoadTurnParams{Owner: owner, UpdateID: updateID})
	if err != nil {
		return SavedPlan{}, core.DatabaseOperationError(err)
	}
	return decodePlan(row)
}

// SaveWinner validates both the candidate and the concurrent winner.
func (s Store) SaveWinner(ctx context.Context, owner string, updateID int64, plan SavedPlan) (SavedPlan, error) {
	if owner == "" {
		return SavedPlan{}, errors.New("missing turn owner")
	}
	if err := plan.Validate(); err != nil {
		return SavedPlan{}, err
	}
	payload, err := json.Marshal(plan)
	if err != nil {
		return SavedPlan{}, err
	}
	row, err := dbgen.New(s.DB).SaveTurnWinner(ctx, dbgen.SaveTurnWinnerParams{
		Owner: owner, UpdateID: updateID, Payload: payload, Kind: string(plan.Kind), State: string(plan.State),
		Reason: string(plan.TerminalReason), HistoryGeneration: plan.HistoryGeneration,
	})
	if err != nil {
		return SavedPlan{}, err
	}
	return decodePlan(row)
}

func decodePlan(row dbgen.InteractionSavedTurn) (SavedPlan, error) {
	plan := SavedPlan{
		Kind:              PlanKind(row.Kind),
		State:             PlanState(row.State),
		TerminalReason:    TerminalReason(row.Reason),
		HistoryGeneration: row.HistoryGeneration,
	}
	if err := json.Unmarshal(row.Payload, &plan); err != nil {
		return plan, err
	}
	return plan, plan.Validate()
}

type TerminalReason string

const (
	HistoryDeleted TerminalReason = "history_deleted"
	SourceRevoked  TerminalReason = "source_revoked"
)

// MarkTerminal atomically fences replay and removes renderable derived outputs.
// Execution receipts are retained so retrying cannot repeat a committed effect.
func (s Store) MarkTerminal(
	ctx context.Context,
	owner string,
	updateID, generation int64,
	reason TerminalReason,
) error {
	if owner == "" || generation < 0 || (reason != HistoryDeleted && reason != SourceRevoked) {
		return errors.New("invalid terminal reason")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := dbgen.New(tx)
	terminal := SavedPlan{
		FormatVersion:     CurrentFormatVersion,
		Kind:              TerminalPlan,
		State:             PrivacyTerminal,
		TerminalReason:    reason,
		HistoryGeneration: generation,
	}
	payload, err := json.Marshal(terminal)
	if err != nil {
		return err
	}
	affected, err := queries.MarkTerminal(
		ctx,
		dbgen.MarkTerminalParams{
			Owner:             owner,
			UpdateID:          updateID,
			Payload:           payload,
			Reason:            string(reason),
			HistoryGeneration: generation,
		},
	)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if affected != 1 {
		return ErrUnsupportedFormat
	}
	if reason == HistoryDeleted {
		err = queries.DeleteDerivedReplies(ctx, dbgen.DeleteDerivedRepliesParams{Owner: owner, UpdateID: updateID})
	} else {
		err = queries.ClearDerivedReplies(ctx, dbgen.ClearDerivedRepliesParams{Owner: owner, UpdateID: updateID})
		if err == nil {
			err = queries.InsertTerminalReply(ctx, dbgen.InsertTerminalReplyParams{Owner: owner, UpdateID: updateID})
		}
	}
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}
