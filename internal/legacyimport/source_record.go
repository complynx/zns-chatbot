package legacyimport

import (
	"context"
	"errors"
	"io"
)

// ErrSourceRecord means the selected record no longer matches its immutable
// archive identity, owner, role, line or canonical hash.
var ErrSourceRecord = errors.New("historical source record mismatch")

// readSourceRecord returns only after validation of the complete source bundle.
// Callers restrict the target role before projecting the selected raw object.
func readSourceRecord(ctx context.Context, input io.Reader, digest string, target Record,
	project func(map[string]any) error,
) (BundleValidation, error) {
	if target.SourceKey == "" || !validTelegramID(target.OwnerID) {
		return BundleValidation{}, ErrSourceRecord
	}
	found := false
	validation, err := visitBundle(ctx, input, digest, nil, func(record Record, object map[string]any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if record.SourceInstance != target.SourceInstance || record.Collection != target.Collection ||
			record.SourceKey != target.SourceKey {
			return nil
		}
		if record.Role != target.Role || record.OwnerID != target.OwnerID || record.Line != target.Line ||
			record.SHA256 != target.SHA256 {
			return ErrSourceRecord
		}
		if err := project(object); err != nil {
			return err
		}
		found = true
		return nil
	})
	if ctx.Err() != nil {
		return BundleValidation{}, ctx.Err()
	}
	if err != nil {
		return BundleValidation{}, err
	}
	if !found {
		return BundleValidation{}, ErrSourceRecord
	}
	return validation, nil
}
