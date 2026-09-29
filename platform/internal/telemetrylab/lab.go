// Package telemetrylab provides a bounded, synthetic-only OTLP capture stand.
// It must be served on loopback. It does not forward data or redact evidence.
package telemetrylab

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const MaxWireBytes = 256 << 10
const maxBatchBytes = 1 << 20
const maxRetainedBytes = 4 << 20
const maxSettingBytes = 64

type Lab struct {
	mu       sync.Mutex
	batches  []json.RawMessage
	bytes    int
	received uint64
	fail     bool
}

func (l *Lab) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/traces", l.ingest)
	mux.HandleFunc("GET /lab/traces", l.snapshot)
	mux.HandleFunc("POST /lab/failure", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Sandbox") != "1" {
			http.Error(w, "sandbox header required", http.StatusForbidden)
			return
		}
		var setting struct {
			Enabled bool `json:"enabled"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSettingBytes))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&setting) != nil || decoder.Decode(new(any)) != io.EOF {
			http.Error(w, "invalid setting", http.StatusBadRequest)
			return
		}
		l.mu.Lock()
		l.fail = setting.Enabled
		l.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func (l *Lab) ingest(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxWireBytes))
	if err != nil {
		http.Error(w, "batch too large", http.StatusRequestEntityTooLarge)
		return
	}
	var batch collector.ExportTraceServiceRequest
	if proto.Unmarshal(data, &batch) != nil {
		http.Error(w, "invalid protobuf", http.StatusBadRequest)
		return
	}
	encoded, err := protojson.Marshal(&batch)
	if err != nil || len(encoded) > maxBatchBytes {
		http.Error(w, "batch too large", http.StatusRequestEntityTooLarge)
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fail {
		http.Error(w, "synthetic collector failure", http.StatusServiceUnavailable)
		return
	}
	l.received++
	l.bytes += len(encoded)
	l.batches = append(l.batches, encoded)
	const maxBatches = 64
	for l.bytes > maxRetainedBytes || len(l.batches) > maxBatches {
		l.bytes -= len(l.batches[0])
		l.batches[0] = nil
		l.batches = l.batches[1:]
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
}

func (l *Lab) snapshot(w http.ResponseWriter, _ *http.Request) {
	l.mu.Lock()
	value := struct {
		Batches       []json.RawMessage `json:"batches"`
		Received      uint64            `json:"received"`
		RetainedBytes int               `json:"retained_bytes"`
	}{append([]json.RawMessage{}, l.batches...), l.received, l.bytes}
	l.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}
