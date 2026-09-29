package agent

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeScriptedMode(w http.ResponseWriter, r *http.Request) (string, error) {
	const maxModeBytes = 64 << 10
	r.Body = http.MaxBytesReader(w, r.Body, maxModeBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		Mode string `json:"mode"`
	}
	if err := decoder.Decode(&input); err != nil {
		return "", err
	}
	var tail any
	if !errors.Is(decoder.Decode(&tail), io.EOF) {
		return "", errors.New("trailing JSON")
	}
	return input.Mode, nil
}
