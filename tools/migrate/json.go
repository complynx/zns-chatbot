package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func validJSON(data []byte) error {
	if !utf8.Valid(data) {
		return errors.New("invalid_utf8")
	}
	if !validUnicodeEscapes(data) {
		return errors.New("invalid_unicode_escape")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := jsonValue(decoder, 0); err != nil {
		return errors.New("invalid_json")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing_json")
	}
	return nil
}

func jsonValue(decoder *json.Decoder, depth int) error {
	if depth > maxDepth {
		return errors.New("json_depth_exceeded")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	seen := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			key, keyErr := decoder.Token()
			if keyErr != nil {
				return keyErr
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("duplicate_json_key")
			}
			seen[name] = true
		}
		if err = jsonValue(decoder, depth+1); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func decodeManifest(data []byte) (Manifest, error) {
	var value Manifest
	if err := validJSON(data); err != nil {
		return value, err
	}
	if err := manifestKeys(data); err != nil {
		return value, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, errors.New("invalid_manifest_json")
	}
	return value, nil
}

// Identity canonicalization does not alter source bytes. Extended JSON values
// elsewhere in the document remain opaque until a domain converter owns them.
func recordID(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || len(raw) > 1024 || validJSON(raw) != nil {
		return "", errors.New("invalid_record_id")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", errors.New("invalid_record_id")
	}
	switch id := value.(type) {
	case string:
		if id == "" || strings.ContainsRune(id, utf8.RuneError) {
			return "", errors.New("empty_record_id")
		}
		return "string:" + id, nil
	case json.Number:
		number, err := strconv.ParseInt(string(id), 10, 64)
		if err != nil {
			return "", errors.New("invalid_numeric_id")
		}
		return "integer:" + strconv.FormatInt(number, 10), nil
	case map[string]any:
		return extendedID(id)
	default:
		return "", errors.New("unsupported_record_id")
	}
}

func extendedID(id map[string]any) (string, error) {
	if len(id) != 1 {
		return "", errors.New("unsupported_record_id")
	}
	if raw, ok := id["$oid"].(string); ok {
		if len(raw) != 24 || strings.Trim(raw, "0123456789abcdefABCDEF") != "" {
			return "", errors.New("invalid_object_id")
		}
		return "oid:" + strings.ToLower(raw), nil
	}
	for _, key := range []string{"$numberLong", "$numberInt"} {
		if raw, ok := id[key].(string); ok {
			bits := 64
			if key == "$numberInt" {
				bits = 32
			}
			number, err := strconv.ParseInt(raw, 10, bits)
			if err != nil {
				return "", errors.New("invalid_numeric_id")
			}
			return "integer:" + strconv.FormatInt(number, 10), nil
		}
	}
	return "", errors.New("unsupported_record_id")
}

// encoding/json accepts case aliases by default; manifests require exact names.
func manifestKeys(data []byte) error {
	root, err := objectFields(data, "version snapshot_id bot_id captured_at consistency coverage files proofs")
	if err != nil {
		return err
	}
	sections := map[string]string{
		"coverage": "domain name status reason",
		"files":    "path source kind sha256 bytes records",
		"proofs":   "source record_id owner_id field telegram_file_id chat_id message_id blob unavailable",
	}
	for section, allowed := range sections {
		var records []json.RawMessage
		if raw, present := root[section]; present {
			if err = json.Unmarshal(raw, &records); err != nil {
				return errors.New("invalid_manifest_section")
			}
			for _, record := range records {
				if _, err = objectFields(record, allowed); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func objectFields(data []byte, allowed string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return nil, errors.New("invalid_manifest_object")
	}
	keys := strings.Fields(allowed)
	for key := range fields {
		if !slices.Contains(keys, key) {
			return nil, errors.New("unknown_manifest_field")
		}
	}
	return fields, nil
}

// encoding/json replaces unpaired UTF-16 surrogates with U+FFFD. Validate
// escapes before decoding, while leaving JSON structure to the existing parser.
func validUnicodeEscapes(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		if i+1 >= len(data) {
			return false
		}
		if data[i+1] != 'u' {
			i++
			continue
		}
		value, ok := unicodeEscape(data[i:])
		if !ok {
			return false
		}
		i += unicodeEscapeBytes - 1
		if !utf16.IsSurrogate(value) {
			continue
		}
		next, paired := unicodeEscape(data[i+1:])
		if !paired || utf16.DecodeRune(value, next) == utf8.RuneError {
			return false
		}
		i += unicodeEscapeBytes
	}
	return true
}

const unicodeEscapeBytes = 6

func unicodeEscape(data []byte) (rune, bool) {
	if len(data) < unicodeEscapeBytes || data[0] != '\\' || data[1] != 'u' {
		return 0, false
	}
	value, err := strconv.ParseUint(string(data[2:unicodeEscapeBytes]), 16, 16)
	return rune(value), err == nil
}
