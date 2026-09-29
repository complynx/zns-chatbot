// Package identity contains the explicitly sandbox-only identity adapter.
// It is not a substitute for Zitadel or an enabled production authentication mode.
package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Claims struct {
	Subject  string `json:"sub"`
	Actor    string `json:"actor"`
	Audience string `json:"aud"`
	Expires  int64  `json:"exp"`
}
type Signer struct{ Key []byte }

const MinKeyBytes = 32

const (
	AliceTelegramID   = 101
	BobTelegramID     = 202
	VisitorTelegramID = 303
)

func (s Signer) Token(subject string) string {
	return s.token(subject, "zns-core")
}

func (s Signer) DeliveryToken() string { return s.token("telegram-delivery", "zns-notifications") }

func (s Signer) token(subject, audience string) string {
	return s.tokenUntil(subject, audience, time.Now().Add(time.Minute))
}

func (s Signer) tokenUntil(subject, audience string, expires time.Time) string {
	b, _ := json.Marshal(Claims{subject, "sandbox-bot", audience, expires.Unix()})
	body := base64.RawURLEncoding.EncodeToString(b)
	m := hmac.New(sha256.New, s.Key)
	m.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

var ErrSandboxIdentity = errors.New("invalid sandbox identity")

func (s Signer) Verify(token string) (string, error) {
	return s.verify(token, "zns-core")
}

func (s Signer) VerifyDelivery(token string) error {
	subject, err := s.verify(token, "zns-notifications")
	if err != nil {
		return err
	}
	if subject != "telegram-delivery" {
		return errors.New("invalid delivery identity")
	}
	return nil
}

func (s Signer) verify(token, audience string) (string, error) {
	bad := ErrSandboxIdentity
	if len(s.Key) < MinKeyBytes {
		return "", bad
	}
	a, b, ok := strings.Cut(token, ".")
	if !ok {
		return "", bad
	}
	sig, e := base64.RawURLEncoding.DecodeString(b)
	if e != nil {
		return "", bad
	}
	m := hmac.New(sha256.New, s.Key)
	m.Write([]byte(a))
	if !hmac.Equal(sig, m.Sum(nil)) {
		return "", bad
	}
	raw, e := base64.RawURLEncoding.DecodeString(a)
	if e != nil {
		return "", bad
	}
	var c Claims
	if json.Unmarshal(raw, &c) != nil || c.Subject == "" || c.Actor != "sandbox-bot" || c.Audience != audience ||
		c.Expires <= time.Now().Unix() {
		return "", bad
	}
	return c.Subject, nil
}
func Subject(telegramID int64) (string, bool) {
	switch telegramID {
	case AliceTelegramID:
		return "alice", true
	case BobTelegramID:
		return "bob", true
	case VisitorTelegramID:
		return "visitor", true
	}
	return "", false
}
