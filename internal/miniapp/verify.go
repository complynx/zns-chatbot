// Package miniapp validates Telegram proofs and derives application credentials.
// IAM credentials never belong to this browser-facing protocol.
package miniapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

const (
	MaxLoginAge      = 15 * time.Minute
	LoginFutureSkew  = 30 * time.Second
	MaxInitDataBytes = 16 << 10
	maxProofFields   = 32
	maxUserBytes     = 8 << 10
	maxTelegramID    = int64(4503599627370495)
	maxJSONDepth     = 16
)

var ErrInvalid = errors.New("invalid Mini App authentication proof")

type Proof struct {
	User     telegram.User
	AuthDate time.Time
	Digest   [sha256.Size]byte
}

// Verify uses the bot-token HMAC protocol, not Telegram Login Widget or Ed25519.
// It authenticates the original decoded values before interpreting user JSON.
func Verify(raw, botToken string, botID int64, now time.Time, maxAge, futureSkew time.Duration) (Proof, error) {
	if len(raw) == 0 || len(raw) > MaxInitDataBytes || !utf8.ValidString(raw) || botID <= 0 || botID > maxTelegramID ||
		maxAge <= 0 || maxAge > 24*time.Hour || futureSkew < 0 || futureSkew > time.Minute ||
		!strings.HasPrefix(
			botToken,
			strconv.FormatInt(botID, 10)+":",
		) || len(botToken) <= len(strconv.FormatInt(botID, 10))+1 {
		return Proof{}, ErrInvalid
	}
	fields, err := url.ParseQuery(raw)
	if err != nil || len(fields) < 3 || len(fields) > maxProofFields {
		return Proof{}, ErrInvalid
	}
	canonical, err := checkString(fields)
	if err != nil {
		return Proof{}, err
	}
	hash, err := hex.DecodeString(fields.Get("hash"))
	if err != nil || len(hash) != sha256.Size {
		return Proof{}, ErrInvalid
	}
	key := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = key.Write([]byte(botToken))
	mac := hmac.New(sha256.New, key.Sum(nil))
	_, _ = mac.Write([]byte(canonical))
	if !hmac.Equal(hash, mac.Sum(nil)) {
		return Proof{}, ErrInvalid
	}
	seconds, err := strconv.ParseInt(fields.Get("auth_date"), 10, 64)
	if err != nil || seconds <= 0 {
		return Proof{}, ErrInvalid
	}
	authDate := time.Unix(seconds, 0).UTC()
	if authDate.After(now.Add(futureSkew)) || !authDate.Add(maxAge).After(now) {
		return Proof{}, ErrInvalid
	}
	user, err := verifiedUser(fields.Get("user"))
	if err != nil {
		return Proof{}, err
	}
	return Proof{User: user, AuthDate: authDate, Digest: sha256.Sum256([]byte(canonical))}, nil
}

func checkString(fields url.Values) (string, error) {
	keys := make([]string, 0, len(fields))
	for key, values := range fields {
		if key == "" || len(values) != 1 || !utf8.ValidString(key) || strings.ContainsAny(key, "\x00\r\n=") ||
			!utf8.ValidString(values[0]) || strings.ContainsAny(values[0], "\x00\r\n") {
			return "", ErrInvalid
		}
		if key != "hash" {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+fields.Get(key))
	}
	return strings.Join(parts, "\n"), nil
}

func verifiedUser(raw string) (telegram.User, error) {
	if raw == "" || len(raw) > maxUserBytes || !utf8.ValidString(raw) {
		return telegram.User{}, ErrInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := uniqueJSONValue(decoder, 0); err != nil {
		return telegram.User{}, ErrInvalid
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return telegram.User{}, ErrInvalid
	}
	var user telegram.User
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return user, ErrInvalid
	}
	// Read exact Telegram field names; encoding/json's struct matching otherwise
	// accepts aliases such as ID, which must not override the signed id field.
	for name, target := range map[string]any{
		"id": &user.ID, "is_bot": &user.IsBot, "first_name": &user.FirstName,
		"last_name": &user.LastName, "username": &user.Username, "language_code": &user.LanguageCode,
	} {
		if value, present := fields[name]; present {
			if err := json.Unmarshal(value, target); err != nil {
				return telegram.User{}, ErrInvalid
			}
		}
	}
	if user.ID <= 0 || user.ID > maxTelegramID || user.IsBot {
		return telegram.User{}, ErrInvalid
	}
	return user, nil
}

// uniqueJSONValue rejects duplicate keys at every depth, including escaped aliases.
func uniqueJSONValue(decoder *json.Decoder, depth int) error {
	if depth > maxJSONDepth {
		return ErrInvalid
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, container := token.(json.Delim)
	if !container {
		return nil
	}
	if delim != '{' && delim != '[' {
		return ErrInvalid
	}
	seen := make(map[string]bool)
	for decoder.More() {
		if delim == '{' {
			name, keyErr := decoder.Token()
			if keyErr != nil {
				return keyErr
			}
			key, ok := name.(string)
			if !ok || seen[key] {
				return ErrInvalid
			}
			seen[key] = true
		}
		if err = uniqueJSONValue(decoder, depth+1); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}
