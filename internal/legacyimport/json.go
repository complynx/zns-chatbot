package legacyimport

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

const maxDepth = 64

var errInvalidJSON = errors.New("invalid snapshot JSON")

// decodeJSON preserves integer precision and rejects ambiguous duplicate keys.
func decodeJSON(raw []byte) (any, error) {
	if !utf8.Valid(raw) {
		return nil, errInvalidJSON
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := decodeValue(d, 0)
	if err != nil {
		return nil, errInvalidJSON
	}
	if _, err = d.Token(); !errors.Is(err, io.EOF) {
		return nil, errInvalidJSON
	}
	return v, nil
}

func decodeValue(d *json.Decoder, depth int) (any, error) {
	if depth > maxDepth {
		return nil, errInvalidJSON
	}
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		return decodeObject(d, depth+1)
	case json.Delim('['):
		return decodeArray(d, depth+1)
	case json.Delim('}'), json.Delim(']'):
		return nil, errInvalidJSON
	default:
		return token, nil
	}
}

func decodeObject(d *json.Decoder, depth int) (map[string]any, error) {
	object := make(map[string]any)
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, errInvalidJSON
		}
		if _, exists := object[key]; exists {
			return nil, errInvalidJSON
		}
		value, err := decodeValue(d, depth)
		if err != nil {
			return nil, err
		}
		object[key] = value
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') {
		return nil, errInvalidJSON
	}
	return object, nil
}

func decodeArray(d *json.Decoder, depth int) ([]any, error) {
	values := make([]any, 0)
	for d.More() {
		value, err := decodeValue(d, depth)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	end, err := d.Token()
	if err != nil || end != json.Delim(']') {
		return nil, errInvalidJSON
	}
	return values, nil
}
