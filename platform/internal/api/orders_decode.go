package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// DecodeOrderRequest reserves a bounded command envelope for 256 KiB canonical
// choices. Unrelated API and gateway routes retain the shared decoder.
func DecodeOrderRequest(w http.ResponseWriter, r *http.Request, target any) error {
	const maxOrderRequestBytes = 320 << 10
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxOrderRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("trailing order JSON")
	}
	return nil
}
