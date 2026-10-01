package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const allocatedURL = "postgres://postgres:private@127.0.0.1:25432/synthetic_qa_zns_guard?sslmode=disable"

func ownerProjection() projection {
	return projection{Host: "127.0.0.1", Port: 25432, Database: "synthetic_qa_zns_guard",
		Role: "postgres", Transport: "host-loopback", Marker: marker}
}

func TestOwnerConfigExactAuthority(t *testing.T) {
	spec := ownerProjection()
	for _, dsn := range []string{allocatedURL, strings.TrimSuffix(allocatedURL, "?sslmode=disable")} {
		t.Setenv("MIGRATE_DATABASE_URL", dsn)
		config, err := ownerConfig(spec)
		require.NoError(t, err)
		require.Equal(t, spec.Host, config.Host)
		require.Equal(t, spec.Port, config.Port)
		require.Equal(t, spec.Database, config.Database)
		require.Equal(t, spec.Role, config.User)
		require.Empty(t, config.Fallbacks)
		require.Nil(t, config.TLSConfig)
		require.Empty(t, config.RuntimeParams)
		addresses, lookupErr := config.LookupFunc(t.Context(), spec.Host)
		require.NoError(t, lookupErr)
		require.Equal(t, []string{spec.Host}, addresses)
		_, lookupErr = config.LookupFunc(t.Context(), "localhost")
		require.EqualError(t, lookupErr, targetError)
	}
}

func TestOwnerConfigRejectsOtherTargetsBeforeConnection(t *testing.T) {
	for _, dsn := range []string{
		strings.Replace(allocatedURL, "25432", "25433", 1),
		strings.Replace(allocatedURL, "127.0.0.1", "localhost", 1),
		strings.Replace(allocatedURL, "postgres:private", "zns_app:private", 1),
		strings.Replace(allocatedURL, "_guard", "_copy", 1),
		strings.Replace(allocatedURL, "127.0.0.1:25432", "127.0.0.1:25432,127.0.0.2:25433", 1),
		strings.Replace(allocatedURL, "sslmode=disable", "sslmode=prefer", 1),
		allocatedURL + "&host=127.0.0.2", allocatedURL + "&port=25433", allocatedURL + "&dbname=production",
		allocatedURL + "&user=zns_app", allocatedURL + "&hostaddr=127.0.0.2", allocatedURL + "&service=other",
		allocatedURL + "&sslmode=disable", allocatedURL + "#private", allocatedURL + "\n",
		"host=127.0.0.1 port=25432 dbname=synthetic_qa_zns_guard user=postgres", "service=other",
		"postgres://postgres@/synthetic_qa_zns_guard?host=/tmp",
	} {
		t.Setenv("MIGRATE_DATABASE_URL", dsn)
		config, err := ownerConfig(ownerProjection())
		require.Nil(t, config)
		require.EqualError(t, err, targetError)
	}
	t.Setenv("MIGRATE_DATABASE_URL", allocatedURL)
	for _, key := range []string{"PGHOST", "PGHOSTADDR", "PGPORT", "PGDATABASE", "PGUSER", "PGSERVICE", "PGSSLMODE", "PGOPTIONS", "pgService"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "private")
			config, err := ownerConfig(ownerProjection())
			require.Nil(t, config)
			require.EqualError(t, err, targetError)
		})
	}
}

func TestOwnerRunGuardsBeforeReadingOrConnecting(t *testing.T) {
	spec := ownerProjection()
	body, err := json.Marshal(spec)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "owner.json")
	require.NoError(t, os.WriteFile(path, body, 0o600))
	sum := sha256.Sum256(body)
	args := []string{"sql-client", path, hex.EncodeToString(sum[:])}
	t.Setenv("MIGRATE_DATABASE_URL", strings.Replace(allocatedURL, "25432", "25433", 1))
	output, err := run(args, nil)
	require.Nil(t, output)
	require.EqualError(t, err, targetError)
	t.Setenv("MIGRATE_DATABASE_URL", allocatedURL)
	for _, query := range []string{"", strings.Repeat("q", inputLimit+1), "SELECT '\x00';", "\xff"} {
		output, err = run(args, strings.NewReader(query))
		require.Nil(t, output)
		require.EqualError(t, err, "bounded_utf8_owner_query_required")
	}
	require.NoError(t, os.WriteFile(path, append(body, ' '), 0o600))
	output, err = run(args, nil)
	require.Nil(t, output)
	require.EqualError(t, err, "reviewed_owner_projection_required")
}

func TestOwnerRowsStayBoundedAndSingleColumn(t *testing.T) {
	var output bytes.Buffer
	require.NoError(t, appendRow(&output, [][]byte{[]byte("safe")}))
	require.Equal(t, "safe\n", output.String())
	for _, values := range [][][]byte{nil, {[]byte("a"), []byte("b")}, {{0xff}}, {bytes.Repeat([]byte("x"), outputLimit)}} {
		previous := output.String()
		require.EqualError(t, appendRow(&output, values), "bounded_single_column_owner_result_required")
		require.Equal(t, previous, output.String())
	}
	output.Reset()
	require.NoError(t, appendRow(&output, [][]byte{bytes.Repeat([]byte("x"), outputLimit-1)}))
	require.Equal(t, outputLimit, output.Len())
	require.Error(t, appendRow(&output, [][]byte{nil}))
	require.Equal(t, outputLimit, output.Len())
}
