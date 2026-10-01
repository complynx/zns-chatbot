package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const allocatedURL = "postgres://zns_app:private@127.0.0.1:25432/synthetic_qa_zns_guard?sslmode=disable"

func TestRuntimeConfig(t *testing.T) {
	cases := []struct{ name, dsn string }{
		{"wrong_host", strings.Replace(allocatedURL, "127.0.0.1", "127.0.0.2", 1)},
		{"wrong_port", strings.Replace(allocatedURL, "25432", "25433", 1)},
		{"copied_database_elsewhere", strings.Replace(allocatedURL, "25432", "25434", 1)},
		{"wrong_database", strings.Replace(allocatedURL, "_guard", "_other", 1)},
		{"wrong_role", strings.Replace(allocatedURL, "zns_app", "postgres", 1)},
		{"host_query", allocatedURL + "&host=127.0.0.2"},
		{"port_query", allocatedURL + "&port=25433"},
		{"database_query", allocatedURL + "&dbname=synthetic_qa_zns_other"},
		{"user_query", allocatedURL + "&user=postgres"},
		{"hostaddr_query", allocatedURL + "&hostaddr=127.0.0.2"},
		{"service_query", allocatedURL + "&service=other"},
		{"duplicate_ssl", allocatedURL + "&sslmode=disable"},
		{"fallback", strings.Replace(allocatedURL, "sslmode=disable", "sslmode=prefer", 1)},
		{"multihost", strings.Replace(allocatedURL, "127.0.0.1:25432", "127.0.0.1:25432,127.0.0.2:25433", 1)},
		{"keyword", "host=127.0.0.1 port=25432 dbname=synthetic_qa_zns_guard user=zns_app"},
		{"service", "service=other"},
		{"missing password", strings.Replace(allocatedURL, ":private", "", 1)},
		{"empty password", strings.Replace(allocatedURL, ":private", ":", 1)},
		{"socket", "postgres://zns_app@/synthetic_qa_zns_guard?host=/tmp"},
		{"fragment", allocatedURL + "#other"},
		{"empty", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("E_RUNTIME_DATABASE_URL", test.dsn)
			config, err := runtimeConfig("127.0.0.1", 25432, "synthetic_qa_zns_guard", "zns_app", "host-loopback")
			require.Nil(t, config)
			require.EqualError(t, err, "allocated_runtime_target_required")
		})
	}
	for _, dsn := range []string{allocatedURL, strings.TrimSuffix(allocatedURL, "?sslmode=disable")} {
		t.Setenv("E_RUNTIME_DATABASE_URL", dsn)
		config, err := runtimeConfig("127.0.0.1", 25432, "synthetic_qa_zns_guard", "zns_app", "host-loopback")
		require.NoError(t, err)
		require.Equal(t, "private", config.ConnConfig.Password)
		require.Empty(t, config.ConnConfig.Fallbacks)
		require.Nil(t, config.ConnConfig.TLSConfig)
	}
	t.Setenv("E_RUNTIME_DATABASE_URL", strings.Replace(allocatedURL, "127.0.0.1", "localhost", 1))
	config, err := runtimeConfig("localhost", 25432, "synthetic_qa_zns_guard", "zns_app", "host-loopback")
	require.Nil(t, config)
	require.EqualError(t, err, "allocated_runtime_target_required")
}

func TestPGEnvironmentRejected(t *testing.T) {
	for _, key := range []string{"PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGSERVICE", "PGSERVICEFILE", "PGHOSTADDR", "PGSSLMODE", "PGPASSFILE"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv("E_RUNTIME_DATABASE_URL", allocatedURL)
			t.Setenv(key, "indirection")
			config, err := runtimeConfig("127.0.0.1", 25432, "synthetic_qa_zns_guard", "zns_app", "host-loopback")
			require.Nil(t, config)
			require.EqualError(t, err, "allocated_runtime_target_required")
		})
	}
}

func TestUnboundTransportRejected(t *testing.T) {
	t.Setenv("E_RUNTIME_DATABASE_URL", allocatedURL)
	for _, transport := range []string{"", "network", "unix"} {
		config, err := runtimeConfig("127.0.0.1", 25432, "synthetic_qa_zns_guard", "zns_app", transport)
		require.Nil(t, config)
		require.EqualError(t, err, "allocated_runtime_target_required")
	}
}

func TestRunRejectsTargetBeforePool(t *testing.T) {
	spec := input{
		Host:         "127.0.0.1",
		Port:         25432,
		Database:     "synthetic_qa_zns_guard",
		Role:         "zns_app",
		Transport:    "host-loopback",
		Marker:       "qa.e-import-removal.20261001.synthetic-only",
		Owner:        "one",
		OtherOwner:   "two",
		Event:        "event",
		Draft:        "draft",
		HistoryID:    1,
		MassageCount: 1,
		TextSHA256:   strings.Repeat("a", 64),
		DraftSHA256:  strings.Repeat("b", 64),
	}
	raw, err := json.Marshal(spec)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "projection.json")
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	sum := sha256.Sum256(raw)
	arguments := []string{"probe", "delete", path, hex.EncodeToString(sum[:])}
	t.Setenv("E_RUNTIME_DATABASE_URL", strings.Replace(allocatedURL, "25432", "25433", 1))
	// The exact guard error is returned before NewWithConfig or any SQL/mutation.
	require.EqualError(t, run(arguments), "allocated_runtime_target_required")
	for _, dsn := range []string{strings.Replace(allocatedURL, ":private", "", 1), strings.Replace(allocatedURL, ":private", ":", 1)} {
		t.Setenv("E_RUNTIME_DATABASE_URL", dsn)
		require.EqualError(t, run(arguments), "allocated_runtime_target_required")
	}
	spec.Host = "localhost"
	raw, err = json.Marshal(spec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	sum = sha256.Sum256(raw)
	arguments[len(arguments)-1] = hex.EncodeToString(sum[:])
	t.Setenv("E_RUNTIME_DATABASE_URL", strings.Replace(allocatedURL, "127.0.0.1", "localhost", 1))
	require.EqualError(t, run(arguments), "allocated_runtime_target_required")
	arguments[len(arguments)-1] = strings.Repeat("0", 64)
	require.EqualError(t, run(arguments), "reviewed_probe_input_hash_required")
}
