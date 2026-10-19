package legacyimport

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

const RegistrationProjectionVersion = "zinusya-legacy-registration/v1"
const MaxRegistrationKeyBytes = 256
const MaxRegistrationPageSize = 20
const MaxRegistrationFields = 4096

var ErrRegistrationKey = errors.New("invalid historical registration key")
var ErrRegistrationSource = errors.New("historical source record mismatch")
var ErrRegistrationPage = errors.New("invalid historical registration page")
var ErrRegistrationScanLimit = errors.New("historical registration field limit exceeded")

type RegistrationFields struct {
	State     string `json:"state,omitempty"`
	Type      string `json:"type,omitempty"`
	Role      string `json:"role,omitempty"`
	PaidClaim *bool  `json:"paid_claim,omitempty"`
}

// RegistrationClaim is a selected historical object, never a verified
// event identity, current pass or confirmed payment.
type RegistrationClaim struct {
	Kind              string              `json:"kind"`
	ProjectionVersion string              `json:"projection_version"`
	PassKey           string              `json:"pass_key"`
	Status            string              `json:"status"             enum:"missing,unsupported,claim"`
	Fields            *RegistrationFields `json:"fields,omitempty"`
}

func ValidRegistrationKey(key string) bool {
	return key != "" && len(key) <= MaxRegistrationKeyBytes && utf8.ValidString(key) && !strings.ContainsRune(key, 0)
}

// ReadRegistration projects one literal top-level key from a verified users
// source record. Nothing is returned until the whole archive passes validation.
func ReadRegistration(ctx context.Context, input io.Reader, digest string, target Record, key string,
) (RegistrationClaim, BundleValidation, error) {
	if !ValidRegistrationKey(key) {
		return RegistrationClaim{}, BundleValidation{}, ErrRegistrationKey
	}
	var result RegistrationClaim
	validation, err := readRegistrationSource(ctx, input, digest, target, func(object map[string]any) error {
		result = registrationClaim(object, key)
		return nil
	})
	if err != nil {
		return RegistrationClaim{}, BundleValidation{}, err
	}
	return result, validation, nil
}

func readRegistrationSource(ctx context.Context, input io.Reader, digest string, target Record,
	project func(map[string]any) error,
) (BundleValidation, error) {
	if target.Role != roleUsers || target.SourceKey == "" || !validTelegramID(target.OwnerID) {
		return BundleValidation{}, ErrRegistrationSource
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
			return ErrRegistrationSource
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
		return BundleValidation{}, ErrRegistrationSource
	}
	return validation, nil
}

// RegistrationPage contains structurally supported historical candidates. It
// does not identify real events or claim complete coverage of old registrations.
type RegistrationPage struct {
	Kind              string              `json:"kind"`
	ProjectionVersion string              `json:"projection_version"`
	Items             []RegistrationClaim `json:"items"`
	HasMore           bool                `json:"has_more"`
	NextKey           string              `json:"next_key,omitempty"`
}

// ReadRegistrations discovers supported objects without trusting their keys as
// event identities. Every page revalidates the complete archive before return.
func ReadRegistrations(ctx context.Context, input io.Reader, digest string, target Record, after string, limit int,
) (RegistrationPage, BundleValidation, error) {
	if (after != "" && !ValidRegistrationKey(after)) || limit < 1 || limit > MaxRegistrationPageSize {
		return RegistrationPage{}, BundleValidation{}, ErrRegistrationPage
	}
	var result RegistrationPage
	validation, err := readRegistrationSource(ctx, input, digest, target, func(object map[string]any) error {
		var projectErr error
		result, projectErr = registrationPageFromObject(ctx, object, after, limit)
		return projectErr
	})
	if err != nil {
		return RegistrationPage{}, BundleValidation{}, err
	}
	return result, validation, nil
}

func registrationPageFromObject(
	ctx context.Context,
	object map[string]any,
	after string,
	limit int,
) (RegistrationPage, error) {
	if len(object) > MaxRegistrationFields {
		return RegistrationPage{}, ErrRegistrationScanLimit
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		if err := ctx.Err(); err != nil {
			return RegistrationPage{}, err
		}
		if key > after && ValidRegistrationKey(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	result := RegistrationPage{
		Kind:              "embedded_registration_candidates",
		ProjectionVersion: RegistrationProjectionVersion,
		Items:             []RegistrationClaim{},
	}
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return RegistrationPage{}, err
		}
		claim := registrationClaim(object, key)
		if claim.Status != "claim" {
			continue
		}
		if len(result.Items) == limit {
			result.HasMore = true
			result.NextKey = result.Items[len(result.Items)-1].PassKey
			break
		}
		result.Items = append(result.Items, claim)
	}
	return result, nil
}

func registrationClaim(object map[string]any, key string) RegistrationClaim {
	result := RegistrationClaim{
		Kind:              "embedded_registration_claim",
		ProjectionVersion: RegistrationProjectionVersion,
		PassKey:           key,
		Status:            "missing",
	}
	value, present := object[key]
	if !present {
		return result
	}
	result.Status = "unsupported"
	embedded, ok := value.(map[string]any)
	if !ok {
		return result
	}
	var fields RegistrationFields
	for _, item := range []struct {
		key   string
		value *string
	}{{"state", &fields.State}, {"type", &fields.Type}, {"role", &fields.Role}} {
		if raw, exists := embedded[item.key]; exists {
			text, valid := raw.(string)
			if !valid || text == "" || len(text) > memoFieldMaxBytes || strings.ContainsRune(text, 0) {
				return result
			}
			*item.value = text
		}
	}
	if raw, exists := embedded["paid"]; exists {
		paid, valid := raw.(bool)
		if !valid {
			return result
		}
		fields.PaidClaim = &paid
	}
	if fields.State == "" && fields.Type == "" && fields.Role == "" && fields.PaidClaim == nil {
		return result
	}
	result.Status, result.Fields = "claim", &fields
	return result
}
