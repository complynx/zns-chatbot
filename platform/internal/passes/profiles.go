// Package passes manages individual pass profiles, independently of event registrations.
package passes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/broadcastprofile"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type Service struct{ DB *pgxpool.Pool }

type Profile struct {
	Owner         string     `json:"owner"`
	Version       int64      `json:"version"`
	Role          string     `json:"role"`
	LegalName     string     `json:"legal_name"`
	Passport      string     `json:"passport"`
	Frozen        bool       `json:"frozen"`
	Pending       string     `json:"pending"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	PassportAfter bool       `json:"passport_after"`
}

// Command contains no owner: adapters must supply the authenticated actor separately.
// A set command records an explicitly interpreted profile field independently
// of pending prompts. Adapters must not turn arbitrary messages into submissions.
type Command struct {
	Name          string `json:"name"`
	Field         string `json:"field,omitempty"`
	Version       int64  `json:"version"`
	Key           string `json:"key"`
	Origin        string `json:"origin"`
	Value         string `json:"value,omitempty"`
	PassportAfter bool   `json:"passport_after,omitempty"`
}

const columns = `owner,version,role,legal_name,passport,frozen,pending,expires_at,passport_after`
const inputTTL = 15 * time.Minute
const maxValueLength = 300
const fieldLegalName = "legal_name"
const fieldPassport = "passport"
const fieldRole = "role"
const profileBegin = "begin"
const profileCancel = "cancel"

func problem(status int, code string) error { return &core.ProblemError{Status: status, Code: code} }

func scan(row pgx.Row) (Profile, error) {
	var p Profile
	err := row.Scan(
		&p.Owner,
		&p.Version,
		&p.Role,
		&p.LegalName,
		&p.Passport,
		&p.Frozen,
		&p.Pending,
		&p.ExpiresAt,
		&p.PassportAfter,
	)
	return p, err
}

// Get returns only the authenticated owner's profile. Expired pending input remains
// visible so adapters can explain the timeout; it cannot be submitted.
func (s Service) Get(ctx context.Context, actor string) (Profile, error) {
	p, err := scan(s.DB.QueryRow(ctx, `SELECT `+columns+` FROM core.pass_profiles WHERE owner=$1`, actor))
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		err = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.users WHERE id=$1)`, actor).Scan(&exists)
		if err == nil && !exists {
			return Profile{}, problem(http.StatusForbidden, "forbidden")
		}
		return Profile{Owner: actor}, err
	}
	return p, err
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// Execute serializes all owner mutations, including permission changes. Replays
// return the current profile, never an obsolete snapshot of sensitive data.
func (s Service) Execute(ctx context.Context, actor string, c Command) (Profile, error) {
	if err := validate(c); err != nil {
		return Profile{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Profile{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // Cleanup after commit or an operation error.
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT can_book FROM core.users WHERE id=$1 FOR UPDATE`, actor).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !allowed) {
		return Profile{}, problem(http.StatusForbidden, "forbidden")
	}
	if err != nil {
		return Profile{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO core.pass_profiles(owner) VALUES($1) ON CONFLICT DO NOTHING`, actor)
	if err != nil {
		return Profile{}, err
	}
	p, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM core.pass_profiles WHERE owner=$1 FOR UPDATE`, actor))
	if err != nil {
		return Profile{}, err
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		return Profile{}, err
	}
	keyHash, requestHash := digest([]byte(c.Key)), digest(encoded)
	var prior string
	err = tx.QueryRow(ctx, `SELECT request_hash FROM core.pass_profile_operations WHERE owner=$1 AND key_hash=$2`, actor, keyHash).
		Scan(&prior)
	if err == nil {
		if prior != requestHash {
			return Profile{}, problem(http.StatusConflict, "idempotency_conflict")
		}
		return p, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, err
	}
	if c.Version != p.Version {
		return Profile{}, problem(http.StatusConflict, "pass_profile_stale")
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return Profile{}, err
	}
	if err = apply(&p, c, now); err != nil {
		return Profile{}, err
	}
	p.Version++
	_, err = tx.Exec(
		ctx,
		`UPDATE core.pass_profiles SET version=$2,role=$3,legal_name=$4,passport=$5,pending=$6,expires_at=$7,passport_after=$8 WHERE owner=$1`,
		actor,
		p.Version,
		p.Role,
		p.LegalName,
		p.Passport,
		p.Pending,
		p.ExpiresAt,
		p.PassportAfter,
	)
	if err != nil {
		return Profile{}, err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.pass_profile_operations(owner,key_hash,request_hash) VALUES($1,$2,$3)`,
		actor,
		keyHash,
		requestHash,
	)
	if err != nil {
		return Profile{}, err
	}
	if err = recordProfileChanges(ctx, tx, actor, p.Version, c); err != nil {
		return Profile{}, err
	}
	return p, tx.Commit(ctx)
}

func recordProfileChanges(ctx context.Context, tx pgx.Tx, actor string, version int64, c Command) error {
	if err := recordChange(ctx, tx, actor, version, c); err != nil {
		return err
	}
	if c.Name == profileBegin || c.Name == profileCancel {
		return nil
	}
	return broadcastprofile.PassField(ctx, tx, actor, c.Field, strings.TrimSpace(c.Value))
}

func validField(field string) bool {
	return field == fieldRole || field == fieldLegalName || field == fieldPassport
}

func validate(c Command) error {
	if c.Key == "" || len(c.Key) > 200 || !utf8.ValidString(c.Key) || c.Version < 0 ||
		(c.Origin != "manual" && c.Origin != "agent") {
		return problem(http.StatusBadRequest, "pass_profile_invalid")
	}
	switch c.Name {
	case "begin":
		if !validField(c.Field) || c.Value != "" || (c.PassportAfter && c.Field != fieldLegalName) {
			return problem(http.StatusBadRequest, "pass_profile_invalid")
		}
	case "submit", "set":
		if !validField(c.Field) || c.PassportAfter || !utf8.ValidString(c.Value) || strings.ContainsRune(c.Value, 0) ||
			strings.TrimSpace(c.Value) == "" ||
			utf8.RuneCountInString(c.Value) > maxValueLength {
			return problem(http.StatusBadRequest, "pass_profile_invalid")
		}
	case "cancel":
		if c.Field != "" || c.Value != "" || c.PassportAfter {
			return problem(http.StatusBadRequest, "pass_profile_invalid")
		}
	default:
		return problem(http.StatusBadRequest, "pass_profile_invalid")
	}
	return nil
}

func clearPending(p *Profile) { p.Pending = ""; p.ExpiresAt = nil; p.PassportAfter = false }

func apply(p *Profile, c Command, now time.Time) error {
	if c.Name == "cancel" {
		clearPending(p)
		return nil
	}
	if p.Frozen && c.Field != fieldRole {
		return problem(http.StatusConflict, "pass_profile_frozen")
	}
	if c.Name == "begin" {
		p.Pending = c.Field
		expires := now.Add(inputTTL)
		p.ExpiresAt = &expires
		p.PassportAfter = c.PassportAfter
		return nil
	}
	active := p.ExpiresAt != nil && now.Before(*p.ExpiresAt)
	if c.Name == "submit" {
		if p.Pending == "" || p.Pending != c.Field {
			return problem(http.StatusConflict, "pass_profile_no_pending")
		}
		if !active {
			return problem(http.StatusConflict, "pass_profile_expired")
		}
	}
	value := strings.TrimSpace(c.Value)
	switch c.Field {
	case fieldRole:
		if value != "leader" && value != "follower" {
			return problem(http.StatusBadRequest, "pass_profile_invalid")
		}
		p.Role = value
	case fieldLegalName:
		p.LegalName = value
	case fieldPassport:
		p.Passport = value
	}
	if p.Pending != c.Field {
		return nil
	}
	if p.PassportAfter && active {
		p.Pending = fieldPassport
		p.PassportAfter = false
		expires := now.Add(inputTTL)
		p.ExpiresAt = &expires
	} else {
		clearPending(p)
	}
	return nil
}
