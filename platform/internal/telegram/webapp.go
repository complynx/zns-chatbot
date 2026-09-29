package telegram

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const maxInitDataBytes = 16384
const initDataLifetime = time.Hour
const initDataClockSkew = 30 * time.Second

// VerifyWebApp checks Telegram's signed initData before returning its user.
// See https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app.
func VerifyWebApp(raw, token string, now time.Time) (User, error) {
	bad := errors.New("invalid Telegram Mini App identity")
	if len(raw) > maxInitDataBytes || token == "" {
		return User{}, bad
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return User{}, bad
	}
	for _, entries := range values {
		if len(entries) != 1 {
			return User{}, bad
		}
	}
	signature, err := hex.DecodeString(values.Get("hash"))
	if err != nil || !hmac.Equal(signature, webAppHash(values, token)) {
		return User{}, bad
	}
	authDate, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil {
		return User{}, bad
	}
	issued := time.Unix(authDate, 0)
	if issued.After(now.Add(initDataClockSkew)) || issued.Before(now.Add(-initDataLifetime)) {
		return User{}, bad
	}
	var user User
	if json.Unmarshal([]byte(values.Get("user")), &user) != nil || user.ID <= 0 || user.IsBot {
		return User{}, bad
	}
	return user, nil
}

func webAppHash(values url.Values, token string) []byte {
	fields := make([]string, 0, len(values))
	for key, entries := range values {
		if key != "hash" {
			fields = append(fields, key+"="+entries[0])
		}
	}
	slices.Sort(fields)
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(fields, "\n")))
	return mac.Sum(nil)
}
