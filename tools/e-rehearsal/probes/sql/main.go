// Build only in the hash-bound isolated platform module; never deploy with app credentials.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
)

const (
	argumentCount   = 3
	inputLimit      = 1 << 20
	outputLimit     = 16 << 20
	projectionLimit = 64 << 10
	queryTimeout    = 60 * time.Second
	marker          = "qa.e-import-removal.20261001.synthetic-only"
	targetError     = "allocated_owner_target_required"
)

type projection struct {
	Host      string `json:"host"`
	Port      uint16 `json:"port"`
	Role      string `json:"role"`
	Transport string `json:"transport"`
	Database  string `json:"database"`
	Marker    string `json:"marker"`
}

func main() {
	output, err := run(os.Args, os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "e_owner_sql_failed")
		os.Exit(1)
	}
	if _, err = os.Stdout.Write(output); err != nil {
		fmt.Fprintln(os.Stderr, "e_owner_sql_failed")
		os.Exit(1)
	}
}

func loadProjection(path, hash string) (projection, error) {
	file, err := os.Open(path)
	if err != nil {
		return projection{}, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, projectionLimit+1))
	if err != nil {
		return projection{}, err
	}
	sum := sha256.Sum256(body)
	if len(body) > projectionLimit || hex.EncodeToString(sum[:]) != hash {
		return projection{}, errors.New("reviewed_owner_projection_required")
	}
	var spec projection
	if err = json.Unmarshal(body, &spec); err != nil {
		return projection{}, err
	}
	return spec, nil
}

func ownerConfig(spec projection) (*pgx.ConnConfig, error) {
	rejected := errors.New(targetError)
	if spec.Host != "127.0.0.1" || spec.Port < 1024 || spec.Role != "postgres" ||
		spec.Transport != "host-loopback" || spec.Marker != marker ||
		!regexp.MustCompile(`^synthetic_qa_zns_[a-z0-9_]+$`).MatchString(spec.Database) {
		return nil, rejected
	}
	for _, setting := range os.Environ() {
		key, _, _ := strings.Cut(setting, "=")
		if strings.HasPrefix(strings.ToUpper(key), "PG") {
			return nil, rejected
		}
	}
	raw := os.Getenv("MIGRATE_DATABASE_URL")
	if strings.ContainsAny(raw, " \t\r\n") {
		return nil, rejected
	}
	uri, err := url.Parse(raw)
	if err != nil || (uri.Scheme != "postgres" && uri.Scheme != "postgresql") || uri.User == nil ||
		uri.Hostname() != spec.Host || uri.Port() != strconv.Itoa(int(spec.Port)) ||
		uri.Path != "/"+spec.Database || uri.User.Username() != spec.Role ||
		uri.Fragment != "" || uri.ForceQuery || (uri.RawQuery != "" && uri.RawQuery != "sslmode=disable") {
		return nil, rejected
	}
	uri.RawQuery = "sslmode=disable"
	config, err := pgx.ParseConfig(uri.String())
	if err != nil || config.Host != spec.Host || config.Port != spec.Port || config.Database != spec.Database ||
		config.User != spec.Role || config.TLSConfig != nil || len(config.Fallbacks) != 0 || len(config.RuntimeParams) != 0 {
		return nil, rejected
	}
	config.LookupFunc = func(_ context.Context, host string) ([]string, error) {
		if host != spec.Host {
			return nil, rejected
		}
		return []string{spec.Host}, nil
	}
	config.BuildFrontend = func(reader io.Reader, writer io.Writer) *pgproto3.Frontend {
		frontend := pgproto3.NewFrontend(reader, writer)
		frontend.SetMaxBodyLen(outputLimit)
		return frontend
	}
	return config, nil
}

func run(arguments []string, input io.Reader) ([]byte, error) {
	if len(arguments) != argumentCount {
		return nil, errors.New("owner_projection_arguments_required")
	}
	spec, err := loadProjection(arguments[1], arguments[2])
	if err != nil {
		return nil, err
	}
	config, err := ownerConfig(spec)
	if err != nil {
		return nil, err
	}
	query, err := io.ReadAll(io.LimitReader(input, inputLimit+1))
	if err != nil {
		return nil, err
	}
	if len(query) == 0 || len(query) > inputLimit || !utf8.Valid(query) || bytes.IndexByte(query, 0) >= 0 {
		return nil, errors.New("bounded_utf8_owner_query_required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	connection, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	defer func() { _ = connection.Close(ctx) }()
	if err = ownerGuard(ctx, connection, spec); err != nil {
		return nil, err
	}
	return execute(ctx, connection, string(query))
}

func ownerGuard(ctx context.Context, connection *pgx.Conn, spec projection) error {
	var database, role, comment string
	var busy int
	err := connection.QueryRow(ctx, `SELECT current_database(),current_user,
coalesce((SELECT description FROM pg_shdescription WHERE objoid=(SELECT oid FROM pg_database WHERE datname=current_database()) AND classoid='pg_database'::regclass),''),
(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND usename IN('zns_app','zns_meter'))`).Scan(&database, &role, &comment, &busy)
	if err != nil {
		return err
	}
	if database != spec.Database || role != spec.Role || comment != spec.Marker || busy != 0 {
		return errors.New("exclusive_synthetic_owner_required")
	}
	return nil
}

// Buffer only bounded single-column rows. Failed SQL produces no partial stdout.
func execute(ctx context.Context, connection *pgx.Conn, query string) ([]byte, error) {
	results := connection.PgConn().Exec(ctx, query)
	defer func() { _ = results.Close() }()
	var output bytes.Buffer
	for results.NextResult() {
		result := results.ResultReader()
		if len(result.FieldDescriptions()) > 1 {
			return nil, errors.New("bounded_single_column_owner_result_required")
		}
		for result.NextRow() {
			if err := appendRow(&output, result.Values()); err != nil {
				return nil, err
			}
		}
		if _, err := result.Close(); err != nil {
			return nil, err
		}
	}
	if err := results.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func appendRow(output *bytes.Buffer, values [][]byte) error {
	if len(values) != 1 || !utf8.Valid(values[0]) || len(values[0])+1 > outputLimit-output.Len() {
		return errors.New("bounded_single_column_owner_result_required")
	}
	_, _ = output.Write(values[0])
	_ = output.WriteByte('\n')
	return nil
}
