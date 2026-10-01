// Copy into the isolated platform/cmd/e-removal-probe/main.go, never live source.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

const (
	argumentCount = 4
	probeTimeout  = 30 * time.Second
	denialChunk   = 100
	tombstoneMode = "tombstone"
)

type input struct {
	Host         string `json:"host"`
	Port         uint16 `json:"port"`
	Role         string `json:"role"`
	Transport    string `json:"transport"`
	Database     string `json:"database"`
	Marker       string `json:"marker"`
	Owner        string `json:"owner"`
	OtherOwner   string `json:"other_owner"`
	Event        string `json:"event"`
	Draft        string `json:"draft"`
	HistoryID    int64  `json:"history_id"`
	TextSHA256   string `json:"text_sha256"`
	MassageCount int    `json:"massage_count"`
	DraftSHA256  string `json:"draft_sha256"`
}

func main() {
	if err := run(os.Args); err != nil {
		// Do not print provider errors, identities, text or connection details.
		fmt.Fprintln(os.Stderr, "e_runtime_probe_failed")
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, `{"runtime_probe":"passed"}`)
}

func deny(err error) error {
	var problem *core.ProblemError
	if !errors.As(err, &problem) || (problem.Status != 403 && problem.Status != 404) {
		return errors.New("authorization_denial_missing")
	}
	return nil
}

func run(arguments []string) error {
	if len(arguments) != argumentCount {
		return errors.New("mode_and_private_input_required")
	}
	mode := arguments[1]
	if mode != "check" && mode != "delete" && mode != tombstoneMode {
		return errors.New("invalid_mode")
	}
	spec, err := loadInput(arguments[2], arguments[3])
	if err != nil {
		return err
	}
	config, err := runtimeConfig(spec.Host, spec.Port, spec.Database, spec.Role, spec.Transport)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return err
	}
	defer db.Close()
	if err = restrictedDatabase(ctx, db, spec); err != nil {
		return err
	}
	history := conversation.Service{DB: db}
	if err = checkPrivacy(ctx, db, spec); err != nil {
		return err
	}
	if err = checkMassage(ctx, db, spec); err != nil {
		return err
	}
	if err = checkReference(ctx, db, spec); err != nil {
		return err
	}
	if mode == "delete" {
		if err = history.DeleteContent(ctx, spec.Owner, spec.HistoryID); err != nil {
			return err
		}
		mode = tombstoneMode
	}
	if mode == tombstoneMode {
		return checkTombstone(ctx, db, spec)
	}
	return checkFullHistory(ctx, history, spec)
}

func loadInput(path, expectedHash string) (input, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return input{}, err
	}
	defer root.Close()
	raw, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		return input{}, err
	}
	specHash := sha256.Sum256(raw)
	if hex.EncodeToString(specHash[:]) != expectedHash {
		return input{}, errors.New("reviewed_probe_input_hash_required")
	}
	var spec input
	if err = json.Unmarshal(raw, &spec); err != nil {
		return input{}, err
	}
	if !strings.HasPrefix(spec.Database, "synthetic_qa_zns_") ||
		spec.Marker != "qa.e-import-removal.20261001.synthetic-only" {
		return input{}, errors.New("explicit_synthetic_probe_binding_required")
	}
	if spec.Owner == "" || spec.OtherOwner == "" || spec.Owner == spec.OtherOwner ||
		spec.Event == "" || spec.Draft == "" || spec.HistoryID <= 0 || spec.MassageCount <= 0 ||
		len(spec.TextSHA256) != 64 || len(spec.DraftSHA256) != 64 {
		return input{}, errors.New("representative_input_required")
	}
	return spec, nil
}

// runtimeConfig binds the supported loopback transport before pool creation.
func runtimeConfig(host string, port uint16, database, role, transport string) (*pgxpool.Config, error) {
	rejected := errors.New("allocated_runtime_target_required")
	if transport != "host-loopback" || host != "127.0.0.1" || port < 1024 ||
		role != "zns_app" {
		return nil, rejected
	}
	for _, setting := range os.Environ() {
		key, _, _ := strings.Cut(setting, "=")
		if strings.HasPrefix(strings.ToUpper(key), "PG") {
			return nil, rejected
		}
	}
	uri, err := runtimeURL(host, port, database, role)
	if err != nil {
		return nil, err
	}
	password, _ := uri.User.Password()
	config, err := pgxpool.ParseConfig(uri.String())
	if err != nil {
		return nil, rejected
	}
	effective := config.ConnConfig
	if effective.Host != host || effective.Port != port || effective.Database != database || effective.User != role ||
		effective.Password != password || effective.TLSConfig != nil || len(effective.Fallbacks) != 0 || len(effective.RuntimeParams) != 0 {
		return nil, rejected
	}
	// Only the literal allocated IPv4 endpoint is accepted; no alias or DNS lookup.
	effective.LookupFunc = func(_ context.Context, name string) ([]string, error) {
		if name != host {
			return nil, rejected
		}
		return []string{"127.0.0.1"}, nil
	}
	return config, nil
}

func runtimeURL(host string, port uint16, database, role string) (*url.URL, error) {
	raw := os.Getenv("E_RUNTIME_DATABASE_URL")
	if strings.ContainsAny(raw, " \t\r\n") {
		return nil, errors.New("allocated_runtime_target_required")
	}
	uri, err := url.Parse(raw)
	if err != nil || (uri.Scheme != "postgres" && uri.Scheme != "postgresql") || uri.User == nil ||
		uri.Hostname() != host || uri.Port() != strconv.Itoa(int(port)) || uri.Path != "/"+database ||
		uri.User.Username() != role || uri.Fragment != "" || uri.ForceQuery ||
		(uri.RawQuery != "" && uri.RawQuery != "sslmode=disable") {
		return nil, errors.New("allocated_runtime_target_required")
	}
	password, supplied := uri.User.Password()
	if !supplied || password == "" {
		return nil, errors.New("allocated_runtime_target_required")
	}
	// This transport has one plaintext loopback endpoint, never SSL fallback.
	uri.RawQuery = "sslmode=disable"
	return uri, nil
}

func restrictedDatabase(ctx context.Context, db *pgxpool.Pool, spec input) error {
	var database, role, marker string
	var importerAbsent bool
	err := db.QueryRow(ctx, `SELECT current_database(),current_user,to_regnamespace('migrate_import') IS NULL,
coalesce((SELECT description FROM pg_shdescription WHERE objoid=(SELECT oid FROM pg_database
WHERE datname=current_database()) AND classoid='pg_database'::regclass),'')`).
		Scan(&database, &role, &importerAbsent, &marker)
	if err != nil || database != spec.Database || marker != spec.Marker || role != "zns_app" || !importerAbsent {
		return errors.New("removed_importer_restricted_runtime_required")
	}
	return nil
}

func checkPrivacy(ctx context.Context, db *pgxpool.Pool, spec input) error {
	history := conversation.Service{DB: db}
	massages := massage.Service{DB: db}
	_, err := history.ReadText(ctx, spec.OtherOwner, spec.HistoryID, 0, denialChunk, "")
	if err = deny(err); err != nil {
		return err
	}
	err = history.DeleteContent(ctx, spec.OtherOwner, spec.HistoryID)
	if err = deny(err); err != nil {
		return err
	}
	_, err = massages.LegacyDraft(ctx, spec.OtherOwner, spec.Event, spec.Draft)
	if err = deny(err); err != nil {
		return err
	}
	_, err = history.Read(ctx, "e-unknown-owner", conversation.Query{Limit: 1})
	if err = deny(err); err != nil {
		return err
	}
	return nil
}

func checkMassage(ctx context.Context, db *pgxpool.Pool, spec input) error {
	massages := massage.Service{DB: db}
	draft, err := massages.LegacyDraft(ctx, spec.Owner, spec.Event, spec.Draft)
	if err != nil {
		return err
	}
	draftBytes, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	draftHash := sha256.Sum256(draftBytes)
	if hex.EncodeToString(draftHash[:]) != spec.DraftSHA256 {
		return errors.New("imported_massage_draft_changed")
	}
	bookings, err := massages.Bookings(ctx, spec.Owner, spec.Event, "", "mine")
	if err != nil || len(bookings) != spec.MassageCount {
		return errors.New("imported_bookings_missing")
	}
	return nil
}

func checkReference(ctx context.Context, db *pgxpool.Pool, spec input) error {
	var reference bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.legacy_message_references WHERE event_id=$1 AND owner=$2 AND disposition='retained')`,
		spec.HistoryID, spec.Owner).
		Scan(&reference)
	if err != nil || !reference {
		return errors.New("permanent_history_reference_missing")
	}
	return nil
}

func checkTombstone(ctx context.Context, db *pgxpool.Pool, spec input) error {
	var valid bool
	err := db.QueryRow(ctx, `SELECT r.tombstoned AND e.omitted AND e.text='' AND e.omission_reason='deleted'
AND NOT EXISTS(SELECT 1 FROM core.conversation_message_bodies WHERE event_id=e.id)
FROM core.legacy_message_references r JOIN core.conversation_events e ON e.id=r.event_id
WHERE r.event_id=$1 AND r.owner=$2`, spec.HistoryID, spec.Owner).Scan(&valid)
	if err != nil || !valid {
		return errors.New("current_tombstone_missing")
	}
	history := conversation.Service{DB: db}
	chunk, readErr := history.ReadText(ctx, spec.Owner, spec.HistoryID, 0, denialChunk, "")
	if readErr != nil || !chunk.Omitted || chunk.Text != "" {
		return errors.New("tombstone_content_resurrected")
	}
	return nil
}

func checkFullHistory(ctx context.Context, history conversation.Service, spec input) error {
	hash := sha256.New()
	offset := 0
	for {
		chunk, readErr := history.ReadText(ctx, spec.Owner, spec.HistoryID, offset, conversation.MaxChunkCharacters, "")
		if readErr != nil || chunk.Omitted {
			return errors.New("imported_history_read_failed")
		}
		_, _ = hash.Write([]byte(chunk.Text))
		if !chunk.More {
			break
		}
		if chunk.NextOffset <= offset {
			return errors.New("history_navigation_did_not_advance")
		}
		offset = chunk.NextOffset
	}
	if hex.EncodeToString(hash.Sum(nil)) != spec.TextSHA256 {
		return errors.New("imported_full_text_changed")
	}
	return nil
}
