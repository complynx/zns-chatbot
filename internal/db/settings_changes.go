package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

const settingsChangePreparationTTL = 5 * time.Minute

var ErrSettingsChangeInvalid = errors.New("invalid runtime settings change")

type SettingsChangeArguments struct {
	ExpectedVersion int64    `json:"expected_version" minimum:"1"`
	Document        Settings `json:"document"`
}

type SettingsChangePreparation struct {
	ID          uuid.UUID               `json:"proposal_id"`
	Arguments   SettingsChangeArguments `json:"arguments"`
	Description string                  `json:"description"`
	ExpiresAt   time.Time               `json:"expires_at"`
}

func normalizedSettingsChange(args SettingsChangeArguments, actor string) ([]byte, error) {
	if args.ExpectedVersion < 1 || validateSettings(args.Document, actor) != nil {
		return nil, ErrSettingsChangeInvalid
	}
	raw, err := json.Marshal(args)
	hash := sha256.Sum256(raw)
	return hash[:], err
}

func settingsChangeKey(scope AdminScope, op string) sqlc.GetSettingsChangeProposalParams {
	return sqlc.GetSettingsChangeProposalParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: op,
	}
}

func effectiveSettings(d Settings) (Settings, error) {
	browser, err := d.AvatarBrowserPolicy()
	if err != nil {
		return Settings{}, err
	}
	d.AvatarBrowserStorageLimitBytes = browser.LimitBytes
	d.AvatarBrowserRetentionSeconds = int(browser.Retention / time.Second)
	policy, err := d.MiniAppPolicy()
	if err != nil {
		return Settings{}, err
	}
	long, short, err := d.MassageReminderLeads()
	if err != nil {
		return Settings{}, err
	}
	d.MassageNotifyClientPriorLongSeconds = int(long)
	d.MassageNotifyClientPriorSeconds = int(short)
	if d.MassageMaxBookingsPerWindow == 0 {
		d.MassageMaxBookingsPerWindow = defaultMassageMaxBookingsPerWindow
	}
	if d.NameExpectationSeconds == 0 {
		d.NameExpectationSeconds = defaultNameExpectationSeconds
	}
	d.MiniAppMaxAgeSeconds = int(policy.MaxAge / time.Second)
	d.MiniAppPendingSeconds = int(policy.PendingTTL / time.Second)
	d.MiniAppSessionSeconds = int(policy.SessionTTL / time.Second)
	return d, nil
}

func settingsChangeDescription(before VersionedSettings, after Settings) (string, error) {
	oldEffective, err := effectiveSettings(before.Document)
	if err != nil {
		return "", err
	}
	newEffective, err := effectiveSettings(after)
	if err != nil {
		return "", err
	}
	documents, err := json.MarshalIndent(map[string]Settings{
		"previous_stored": before.Document, "previous_effective": oldEffective,
		"requested_stored": after, "requested_effective": newEffective,
	}, "", "  ")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"Replace application-wide runtime settings at version %d. This replaces t"+
			"he full document, not one event or organization. Stored and effective va"+
			"lues:\n%s\nMassage reminder lead changes reschedule pending/retryable remi"+
			"nders and preserve retry delays; sent, unknown and inflight reminders ar"+
			"e unchanged. Browser source retention applies to existing uploads from t"+
			"heir creation time; expired bytes are irrecoverable, while draft metadat"+
			"a, receipts and completed JPEGs remain. Lowering the storage budget bloc"+
			"ks new uploads without quota-driven eviction. Worker concurrency applies"+
			" after application restart. Question timeout is reserved and does not ch"+
			"ange current Loom prompt expiry. Current IAM cache TTL is synchronized i"+
			"n this process; other processes are not coordinated. Startup configurati"+
			"on and secrets are unchanged.",
		before.Version,
		documents,
	), nil
}

// PrepareSettingsChange persists exact approval parameters without changing settings.
func (s *Store) PrepareSettingsChange(
	ctx context.Context, scope AdminScope, op string, args SettingsChangeArguments,
) (SettingsChangePreparation, error) {
	hash, err := normalizedSettingsChange(args, scope.Subject)
	if err != nil {
		return SettingsChangePreparation{}, err
	}
	tx, q, err := s.beginAdminOperation(ctx, scope, "settings-change", op)
	if err != nil {
		return SettingsChangePreparation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prior, err := q.GetSettingsChangeProposal(ctx, settingsChangeKey(scope, op))
	if err == nil {
		if !bytes.Equal(prior.RequestHash, hash) {
			return SettingsChangePreparation{}, ErrConflict
		}
		var prep SettingsChangePreparation
		err = json.Unmarshal(prior.Document, &prep)
		return prep, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return SettingsChangePreparation{}, err
	}
	old, err := q.GetSettings(ctx)
	if err != nil {
		return SettingsChangePreparation{}, err
	}
	if old.Version != args.ExpectedVersion {
		return SettingsChangePreparation{}, ErrConflict
	}
	var before Settings
	if err = json.Unmarshal(old.Document, &before); err != nil {
		return SettingsChangePreparation{}, err
	}
	description, err := settingsChangeDescription(
		VersionedSettings{Version: old.Version, Document: before},
		args.Document,
	)
	if err != nil {
		return SettingsChangePreparation{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return SettingsChangePreparation{}, err
	}
	prep := SettingsChangePreparation{
		ID:          uuid.New(),
		Arguments:   args,
		Description: description,
		ExpiresAt:   now.Time.Add(settingsChangePreparationTTL),
	}
	document, err := json.Marshal(prep)
	if err != nil {
		return SettingsChangePreparation{}, err
	}
	err = q.InsertSettingsChangeProposal(ctx, sqlc.InsertSettingsChangeProposalParams{
		BotID:          scope.BotID,
		OrganizationID: scope.Organization,
		Actor:          scope.Subject,
		OperationID:    op,
		ProposalID:     prep.ID,
		RequestHash:    hash,
		ExpiresAt:      pgtype.Timestamptz{Time: prep.ExpiresAt, Valid: true},
		Document:       document,
	})
	if err == nil {
		err = tx.Commit(ctx)
	}
	return prep, err
}

// ConfirmSettingsChange commits settings, audit and original agent receipt together.
// Replay returns that original receipt, independently of the current settings version.
func (s *Store) ConfirmSettingsChange(
	ctx context.Context, scope AdminScope, op string, args SettingsChangeArguments, proposal uuid.UUID,
) (VersionedSettings, error) {
	hash, err := normalizedSettingsChange(args, scope.Subject)
	if err != nil {
		return VersionedSettings{}, err
	}
	if proposal == uuid.Nil {
		return VersionedSettings{}, ErrSettingsChangeInvalid
	}
	tx, q, err := s.beginAdminOperation(ctx, scope, "settings-change", op)
	if err != nil {
		return VersionedSettings{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prior, err := q.GetSettingsChangeProposal(ctx, settingsChangeKey(scope, op))
	if err != nil {
		return VersionedSettings{}, err
	}
	if prior.ProposalID != proposal || !bytes.Equal(prior.RequestHash, hash) {
		return VersionedSettings{}, ErrConflict
	}
	if len(prior.Result) != 0 {
		var result VersionedSettings
		err = json.Unmarshal(prior.Result, &result)
		return result, err
	}
	// Sample expiry only after acquiring the settings row lock, including waits.
	current, err := q.LockSettings(ctx)
	if err != nil {
		return VersionedSettings{}, err
	}
	if current.Version != args.ExpectedVersion {
		return VersionedSettings{}, ErrConflict
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return VersionedSettings{}, err
	}
	if !now.Time.Before(prior.ExpiresAt.Time) {
		return VersionedSettings{}, ErrConflict
	}
	result, err := compareAndSwapSettingsTx(ctx, q, args.ExpectedVersion, args.Document, scope.Subject)
	if err != nil {
		return VersionedSettings{}, err
	}
	receipt, err := json.Marshal(result)
	if err != nil {
		return VersionedSettings{}, err
	}
	saved, err := q.SaveSettingsChangeResult(ctx, sqlc.SaveSettingsChangeResultParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: op, Result: receipt,
	})
	if err == nil && saved != 1 {
		err = ErrConflict
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	return result, err
}

// SettingsChangeReceipt exposes only this active administrator's agent receipt.
func (s *Store) SettingsChangeReceipt(ctx context.Context, scope AdminScope, op string) (*VersionedSettings, error) {
	tx, q, err := s.beginAdminOperation(ctx, scope, "settings-change", op)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := q.GetSettingsChangeProposal(ctx, settingsChangeKey(scope, op))
	if err != nil {
		return nil, err
	}
	if len(row.Result) == 0 {
		return nil, pgx.ErrNoRows
	}
	var result VersionedSettings
	err = json.Unmarshal(row.Result, &result)
	return &result, err
}
