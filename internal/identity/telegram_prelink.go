package identity

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/sync/singleflight"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

const (
	prelinkProofLimit   = 16 << 10
	prelinkTextLimit    = 200
	prelinkJWKSKeyLimit = 16
	prelinkMinRSA       = 2048
	prelinkMaxRSA       = 8192
	prelinkLifetime     = time.Minute
	prelinkSkew         = 5 * time.Second
	prelinkKeyRefresh   = 5 * time.Second
	prelinkPurpose      = "telegram-prelink"
)

// TelegramPrelinkOptions pins provider trust separately from IAM token trust.
// The audience must be dedicated to this backchannel, not the IAM relay JWT.
type TelegramPrelinkOptions struct {
	Issuer, JWKSURL, Audience string
	BotID                     int64
}

// TelegramPrelinkProof is authenticated provider intent, not a completed link.
// The API must add its configured organization/IdP and recheck current authority.
type TelegramPrelinkProof struct {
	User                         telegram.User
	Issuer, JTI, ExternalSubject string
	ExpiresAt                    time.Time
	Digest                       [sha256.Size]byte
}

type TelegramPrelinkVerifier struct {
	options            TelegramPrelinkOptions
	http               *http.Client
	mu                 sync.Mutex
	keys               jose.JSONWebKeySet
	until, nextRefresh time.Time
	flight             singleflight.Group
}

func NewTelegramPrelinkVerifier(o TelegramPrelinkOptions, h *http.Client) (*TelegramPrelinkVerifier, error) {
	if !prelinkHTTPS(o.Issuer) || !prelinkHTTPS(o.JWKSURL) || !prelinkText(o.Audience) ||
		o.BotID <= 0 || o.BotID > maxTelegramID {
		return nil, errors.New("prelink requires HTTPS issuer/JWKS, dedicated audience and bot ID")
	}
	if h == nil {
		h = &http.Client{}
	}
	bounded := *h
	if bounded.Timeout <= 0 || bounded.Timeout > requestTimeout {
		bounded.Timeout = requestTimeout
	}
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	// A provider proof never needs browser cookies or other service credentials.
	bounded.Jar = nil
	return &TelegramPrelinkVerifier{options: o, http: &bounded}, nil
}

func prelinkHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 2048 && u.Scheme == "https" && u.Hostname() != "" &&
		u.User == nil && !u.ForceQuery && u.RawQuery == "" && u.Fragment == "" && u.Opaque == ""
}

// Verify accepts only a compact RS256 assertion from the pinned provider keys.
// Expiry is strict: skew never permits an expired assertion to start IAM work.
func (v *TelegramPrelinkVerifier) Verify(ctx context.Context, raw string, now time.Time) (TelegramPrelinkProof, error) {
	started := time.Now()
	if len(raw) == 0 || len(raw) > prelinkProofLimit || strings.Count(raw, ".") != 2 {
		return TelegramPrelinkProof{}, ErrDenied
	}
	encodedHeader, _, _ := strings.Cut(raw, ".")
	header, err := base64.RawURLEncoding.Strict().DecodeString(encodedHeader)
	if err != nil {
		return TelegramPrelinkProof{}, ErrDenied
	}
	fields, err := prelinkObject(header)
	if err != nil {
		return TelegramPrelinkProof{}, ErrDenied
	}
	kid := prelinkString(fields, "kid")
	if !prelinkText(kid) || prelinkString(fields, "alg") != string(jose.RS256) {
		return TelegramPrelinkProof{}, ErrDenied
	}
	signed, err := jose.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil || len(signed.Signatures) != 1 {
		return TelegramPrelinkProof{}, ErrDenied
	}
	key, err := v.key(ctx, kid)
	if err != nil {
		return TelegramPrelinkProof{}, err
	}
	payload, err := signed.Verify(key)
	if err != nil {
		return TelegramPrelinkProof{}, ErrDenied
	}
	return v.proof(raw, payload, now.Add(time.Since(started)))
}

func (v *TelegramPrelinkVerifier) proof(raw string, payload []byte, now time.Time) (TelegramPrelinkProof, error) {
	fields, err := prelinkObject(payload)
	if err != nil {
		return TelegramPrelinkProof{}, ErrDenied
	}
	issuer, jti := prelinkString(fields, "iss"), prelinkString(fields, "jti")
	bot := prelinkString(fields, "urn:zitadeltg:telegram:bot_id")
	opaque := prelinkString(fields, "urn:zitadeltg:telegram:subject")
	numeric := prelinkString(fields, "urn:zitadeltg:telegram:user_id")
	userID, err := strconv.ParseInt(numeric, 10, 64)
	if err != nil || userID <= 0 || userID > maxTelegramID || strconv.FormatInt(userID, 10) != numeric ||
		bot != strconv.FormatInt(v.options.BotID, 10) || issuer != v.options.Issuer ||
		!prelinkText(jti) || !prelinkText(opaque) ||
		prelinkString(fields, "sub") != "telegram:"+bot+":"+opaque ||
		prelinkString(fields, "urn:zinusya:purpose") != prelinkPurpose ||
		prelinkString(fields, "email") != "tg+"+bot+"+"+numeric+"@telegram.invalid" ||
		string(fields["email_verified"]) != "true" || !prelinkAudience(fields["aud"], v.options.Audience) {
		return TelegramPrelinkProof{}, ErrDenied
	}
	expiry, ok := prelinkTimes(fields, now)
	if !ok {
		return TelegramPrelinkProof{}, ErrDenied
	}
	user := telegram.User{ID: userID}
	for name, target := range map[string]*string{
		"given_name": &user.FirstName, "family_name": &user.LastName, "preferred_username": &user.Username,
	} {
		value, present := fields[name]
		if present && (json.Unmarshal(value, target) != nil || !utf8.ValidString(*target) ||
			len(*target) > prelinkTextLimit || strings.ContainsFunc(*target, unicode.IsControl)) {
			return TelegramPrelinkProof{}, ErrDenied
		}
	}
	return TelegramPrelinkProof{User: user, Issuer: issuer, JTI: jti, ExternalSubject: opaque,
		ExpiresAt: expiry, Digest: sha256.Sum256([]byte(raw))}, nil
}

func prelinkTimes(fields map[string]json.RawMessage, now time.Time) (time.Time, bool) {
	var issued, notBefore, expires int64
	if json.Unmarshal(fields["iat"], &issued) != nil || json.Unmarshal(fields["nbf"], &notBefore) != nil ||
		json.Unmarshal(fields["exp"], &expires) != nil || issued <= 0 || notBefore <= 0 || expires <= issued ||
		expires-issued > int64(
			prelinkLifetime/time.Second,
		) || notBefore > issued || issued-notBefore > int64(prelinkSkew/time.Second) {
		return time.Time{}, false
	}
	expiry := time.Unix(expires, 0).UTC()
	return expiry, time.Unix(issued, 0).Before(now.Add(prelinkSkew+time.Nanosecond)) &&
		time.Unix(notBefore, 0).Before(now.Add(prelinkSkew+time.Nanosecond)) && expiry.After(now)
}

func prelinkAudience(raw json.RawMessage, expected string) bool {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == expected
	}
	var list []string
	return json.Unmarshal(raw, &list) == nil && len(list) == 1 && list[0] == expected
}

func prelinkText(value string) bool {
	return value != "" && len(value) <= prelinkTextLimit && utf8.ValidString(value) &&
		!strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

func prelinkString(fields map[string]json.RawMessage, name string) string {
	var result string
	if json.Unmarshal(fields[name], &result) != nil {
		return ""
	}
	return result
}

// prelinkObject keeps exact field names and rejects duplicate/escaped aliases.
// Relevant claim values are decoded separately; unknown claims grant no authority.
func prelinkObject(raw []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) {
		return nil, ErrDenied
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrDenied
	}
	fields := make(map[string]json.RawMessage)
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return nil, ErrDenied
		}
		key, ok := token.(string)
		if !ok {
			return nil, ErrDenied
		}
		if _, exists := fields[key]; exists {
			return nil, ErrDenied
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, ErrDenied
		}
		fields[key] = value
	}
	if _, err = d.Token(); err != nil {
		return nil, ErrDenied
	}
	if _, err = d.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrDenied
	}
	return fields, nil
}

func (v *TelegramPrelinkVerifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	keys, fresh := v.keys, time.Now().Before(v.until)
	v.mu.Unlock()
	if fresh {
		if key := prelinkKey(keys, kid); key != nil {
			return key, nil
		}
	}
	result := v.flight.DoChan("keys", func() (any, error) {
		return true, v.refreshKeys(ctx, kid)
	})
	select {
	case <-ctx.Done():
		return nil, ErrUnavailable
	case outcome := <-result:
		if outcome.Err != nil {
			return nil, outcome.Err
		}
	}

	v.mu.Lock()
	key := prelinkKey(v.keys, kid)
	fresh = time.Now().Before(v.until)
	v.mu.Unlock()
	if key == nil || !fresh {
		return nil, ErrDenied
	}
	return key, nil
}

// Shared refresh outlives one disconnected waiter, but remains bounded.
func (v *TelegramPrelinkVerifier) refreshKeys(ctx context.Context, kid string) error {
	v.mu.Lock()
	fresh := time.Now().Before(v.until)
	if fresh && prelinkKey(v.keys, kid) != nil {
		v.mu.Unlock()
		return nil
	}
	if time.Now().Before(v.nextRefresh) {
		v.mu.Unlock()
		if fresh {
			return ErrDenied
		}
		return ErrUnavailable
	}
	v.nextRefresh = time.Now().Add(prelinkKeyRefresh)
	v.mu.Unlock()
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), requestTimeout)
	defer cancel()
	fetched, err := v.fetchKeys(refreshCtx)
	if err != nil {
		return err
	}
	v.mu.Lock()
	v.keys, v.until = fetched, time.Now().Add(maxCacheTTL)
	v.mu.Unlock()
	return nil
}

func prelinkKey(keys jose.JSONWebKeySet, kid string) *rsa.PublicKey {
	for _, key := range keys.Keys {
		if key.KeyID == kid {
			public, ok := key.Key.(*rsa.PublicKey)
			if ok {
				return public
			}
		}
	}
	return nil
}

func (v *TelegramPrelinkVerifier) fetchKeys(ctx context.Context) (jose.JSONWebKeySet, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, v.options.JWKSURL, nil)
	if err != nil {
		return jose.JSONWebKeySet{}, ErrUnavailable
	}
	response, err := v.http.Do(request)
	if err != nil {
		return jose.JSONWebKeySet{}, ErrUnavailable
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || response.StatusCode != http.StatusOK || len(raw) > maxResponseBytes {
		return jose.JSONWebKeySet{}, ErrUnavailable
	}
	fields, err := prelinkObject(raw)
	if err != nil {
		return jose.JSONWebKeySet{}, ErrUnavailable
	}
	var entries []json.RawMessage
	if json.Unmarshal(fields["keys"], &entries) != nil || len(entries) == 0 || len(entries) > prelinkJWKSKeyLimit {
		return jose.JSONWebKeySet{}, ErrUnavailable
	}
	result := jose.JSONWebKeySet{}
	seen := make(map[string]bool)
	for _, entry := range entries {
		if _, err = prelinkObject(entry); err != nil {
			return jose.JSONWebKeySet{}, ErrUnavailable
		}
		var key jose.JSONWebKey
		if json.Unmarshal(entry, &key) != nil || !prelinkText(key.KeyID) || seen[key.KeyID] ||
			!key.IsPublic() || (key.Use != "" && key.Use != "sig") || (key.Algorithm != "" && key.Algorithm != string(jose.RS256)) {
			return jose.JSONWebKeySet{}, ErrUnavailable
		}
		public, ok := key.Key.(*rsa.PublicKey)
		if !ok || public.N == nil || public.N.BitLen() < prelinkMinRSA || public.N.BitLen() > prelinkMaxRSA ||
			!key.Valid() {
			return jose.JSONWebKeySet{}, ErrUnavailable
		}
		seen[key.KeyID] = true
		result.Keys = append(result.Keys, key)
	}
	return result, nil
}
