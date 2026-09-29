package migrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Exporter metadata preserves Python's original datetime spelling, which an
// RFC3339 instant alone cannot reconstruct for legacy validation identities.
func (o *OrderCandidate) resolveLegacyValidation(fields map[string]json.RawMessage, dates map[string]time.Time) error {
	raw, provided := fields["_migration"]
	eligible := o.State == orderStatePaid && o.Attempt == "" && (o.ProofFile == "" || o.ProofFile == orderStateCash)
	if !eligible {
		if provided {
			return errors.New("order_legacy_metadata_unexpected")
		}
		return nil
	}
	validated, hasDate := dates["validated_at"]
	if !provided {
		if hasDate {
			return errors.New("order_validation_identity_required")
		}
		o.Attempt = "legacy-validation:true"
		return nil
	}
	metadata, err := objectFields(raw, "payment_reservation_token validated_at_source_offset")
	if err != nil {
		return errors.New("order_legacy_metadata_invalid")
	}
	identity, err := orderString(metadata, "payment_reservation_token", true)
	if err != nil {
		return errors.New("order_validation_identity_required")
	}
	offset, err := orderString(metadata, "validated_at_source_offset", false)
	if err != nil {
		return errors.New("order_validation_offset_invalid")
	}
	if !hasDate {
		if identity != "legacy-validation:true" || offset != "" {
			return errors.New("order_validation_identity_mismatch")
		}
	} else if err = validatePythonValidationIdentity(identity, offset, validated); err != nil {
		return err
	}
	o.Attempt = identity
	return nil
}

func validatePythonValidationIdentity(identity, offset string, validated time.Time) error {
	const prefix = "legacy-validation:"
	if !strings.HasPrefix(identity, prefix) {
		return errors.New("order_validation_identity_mismatch")
	}
	stamp := strings.TrimPrefix(identity, prefix)
	instant := strings.Replace(stamp, " ", "T", 1)
	parsed, err := strictEventInstant(instant + offset)
	if err != nil || !parsed.Equal(validated) {
		return errors.New("order_validation_identity_mismatch")
	}
	canonical := parsed.Format("2006-01-02 15:04:05")
	if parsed.Nanosecond() != 0 {
		canonical += fmt.Sprintf(".%06d", parsed.Nanosecond()/int(time.Microsecond))
	}
	if offset == "" {
		canonical += parsed.Format("-07:00")
	}
	if stamp != canonical {
		return errors.New("order_validation_identity_mismatch")
	}
	return nil
}
