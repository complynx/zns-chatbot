package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Broadcast projection retains source presence and types independently of normalized columns.
func broadcastFields(record map[string]json.RawMessage) map[string]json.RawMessage {
	fields := make(map[string]json.RawMessage)
	for key, value := range record {
		if broadcastField(key) {
			fields[key] = value
		}
	}
	return fields
}

func broadcastField(key string) bool {
	switch key {
	case "user_id", "bot_id", userUsernameField, "first_name", "last_name", "print_name", userLanguageField,
		"known_names", "informal_name", "legal_name", "role", "passport_number", "legal_name_frozen", "massage_specialist":
		return true
	default:
		return strings.HasPrefix(key, "inner_name_") && len(key) > len("inner_name_")
	}
}

func insertBroadcastProfile(ctx context.Context, tx pgx.Tx, user preparedUser) error {
	_, err := tx.Exec(ctx, `INSERT INTO core.admin_broadcast_profiles(owner,source_key,source_hash,fields)
 VALUES($1,$2,$3,$4)`, user.resolution.Owner, user.record.Legacy.Key,
		user.record.Legacy.RecordSHA256, user.record.Candidate.BroadcastFields)
	if err != nil {
		return errors.New("apply_broadcast_profile_conflict")
	}
	return nil
}

func reconcileBroadcastProfile(ctx context.Context, tx pgx.Tx, user preparedUser) error {
	var matched bool
	err := tx.QueryRow(ctx, `SELECT source_key=$2 AND source_hash=$3 AND fields=$4::jsonb
 FROM core.admin_broadcast_profiles WHERE owner=$1`, user.resolution.Owner,
		user.record.Legacy.Key, user.record.Legacy.RecordSHA256, user.record.Candidate.BroadcastFields).Scan(&matched)
	if err != nil || !matched {
		return errors.New("apply_broadcast_profile_reconciliation_failed")
	}
	return nil
}
