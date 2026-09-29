package legacyfood

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// SaveLegacyMenu binds the old versionless wire request once. The current editor
// uses Execute with an explicit version and independent operation key.
func (s Service) SaveLegacyMenu(ctx context.Context, actor, event, key string, meals MealSelection) (Order, error) {
	if len(key) != sha256.Size*2 {
		return Order{}, problem("food_invalid_command")
	}
	command, err := s.bindLegacyMenu(ctx, actor, event, key, meals)
	if err != nil {
		return Order{}, err
	}
	result, err := s.Execute(ctx, actor, command)
	if err != nil {
		return Order{}, err
	}
	current, err := s.Get(ctx, actor, event, result.ID)
	if err != nil {
		return Order{}, err
	}
	if current.Version != result.Version {
		return Order{}, problem("stale_version")
	}
	return result, nil
}

func (s Service) bindLegacyMenu(ctx context.Context, actor, event, key string, meals MealSelection) (Command, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Command{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err = allowed(ctx, tx, actor); err != nil {
		return Command{}, err
	}
	if _, err = s.event(ctx, tx, event, true); err != nil {
		return Command{}, err
	}
	raw, err := json.Marshal(meals)
	if err != nil {
		return Command{}, err
	}
	requestHash := digest(raw)
	var savedHash string
	var command Command
	err = tx.QueryRow(ctx, `SELECT request_hash,command FROM core.food_legacy_menu_bindings WHERE actor=$1 AND event_id=$2 AND key_hash=$3`, actor, event, key).
		Scan(&savedHash, &command)
	if err == nil {
		if savedHash != requestHash {
			return Command{}, problem("idempotency_conflict")
		}
		return command, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Command{}, err
	}
	order, err := loadOrder(ctx, tx, event, "", actor)
	if err != nil && !noOrder(err) {
		return Command{}, err
	}
	command = Command{
		EventID: event,
		OrderID: order.ID,
		Version: order.Version,
		Key:     "legacy-menu:" + key,
		Name:    commandSaveMeals,
		Meals:   meals,
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.food_legacy_menu_bindings(actor,event_id,key_hash,request_hash,command) VALUES($1,$2,$3,$4,$5)`,
		actor,
		event,
		key,
		requestHash,
		command,
	)
	if err != nil {
		return Command{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Command{}, err
	}
	return command, nil
}
