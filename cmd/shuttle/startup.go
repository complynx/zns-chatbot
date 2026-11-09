package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func loadRuntimeSettings(ctx context.Context, store *db.Store) (db.VersionedSettings, error) {
	if err := store.CheckRuntimeAccess(ctx); err != nil {
		return db.VersionedSettings{}, fmt.Errorf("runtime database preflight: %w", err)
	}
	settings, err := store.Settings(ctx)
	if err != nil {
		return db.VersionedSettings{}, errors.New("load runtime settings; run -migrate first")
	}
	return settings, nil
}

type startupIdentity interface {
	CheckAccess(context.Context) (identity.Credential, error)
}

func checkDescriptorAccess(ctx context.Context, machine *identity.MachineTokenSource, lc startupLoom) error {
	credential, err := machine.Token(ctx, time.Minute)
	if err != nil {
		return fmt.Errorf("startup descriptor credentials: %w", err)
	}
	if err = lc.CheckModels(ctx, credential.Token); err != nil {
		return fmt.Errorf("startup descriptor model catalog: %w", err)
	}
	return nil
}

type startupTelegram interface {
	GetMe(context.Context) (telegram.User, error)
}

type startupLoom interface {
	CheckModels(context.Context, string) error
}

// Check only service identities and catalog access before any worker or listener.
// Human exchange and prelink require a real user action and are not probed here.
func checkStartupDependencies(ctx context.Context, botID int64,
	iam startupIdentity, tg startupTelegram, lc startupLoom,
) error {
	bot, err := tg.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("startup Telegram getMe: %w", err)
	}
	if bot.ID != botID || !bot.IsBot {
		return errors.New("startup Telegram bot identity mismatch")
	}
	credential, err := iam.CheckAccess(ctx)
	if err != nil {
		return err
	}
	if err = lc.CheckModels(ctx, credential.Token); err != nil {
		return fmt.Errorf("startup Loom service model catalog: %w", err)
	}
	return nil
}
