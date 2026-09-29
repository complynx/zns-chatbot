package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

func deferUserMassageFields(record map[string]json.RawMessage, planned *UserPlanRecord) {
	if planned.Candidate == nil || planned.Status != userCandidate {
		return
	}
	raw, present := record[massageSpecialistField]
	if !present {
		return
	}
	if _, err := convertMassageSpecialist(record); err != nil {
		return
	}
	planned.DeferredMassage = raw
	planned.Candidate.Notifications = nil
	for i, field := range planned.Fields {
		if field.Field == massageSpecialistField || strings.HasPrefix(field.Field, "massage_specialist.") {
			planned.Fields[i].Disposition = "deferred_massage"
		}
	}
	planned.Blockers = slices.DeleteFunc(
		planned.Blockers,
		func(s string) bool { return s == "specialist_event_mapping_unresolved" },
	)
	unmapped := false
	for _, field := range planned.Fields {
		unmapped = unmapped || field.Disposition == userFieldUnmapped
	}
	if !unmapped {
		planned.Blockers = slices.DeleteFunc(planned.Blockers, func(s string) bool { return s == "unmapped_fields" })
	}
}
func insertUserMassageDeferred(ctx context.Context, tx pgx.Tx, user preparedUser) error {
	if len(user.record.DeferredMassage) == 0 {
		return nil
	}
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO core.legacy_user_deferred_domains(source_key,domain,source_record) VALUES($1,'massage',$2)`,
		user.record.Legacy.Key,
		user.record.DeferredMassage,
	); err != nil {
		return errors.New("apply_deferred_domain_conflict")
	}
	return nil
}
func reconcileUserMassageDeferred(ctx context.Context, tx pgx.Tx, user preparedUser) error {
	if len(user.record.DeferredMassage) == 0 {
		return nil
	}
	var matched bool
	if err := tx.QueryRow(ctx, `SELECT source_record=$2::jsonb FROM core.legacy_user_deferred_domains WHERE source_key=$1 AND domain='massage'`, user.record.Legacy.Key, user.record.DeferredMassage).
		Scan(&matched); err != nil ||
		!matched {
		return errors.New("apply_reconciliation_failed")
	}
	return nil
}
