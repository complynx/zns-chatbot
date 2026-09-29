package identity

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const telegramProvisioningAudience = "zns-telegram-provisioning"
const MaxProvisioningBytes = 8192

// TelegramProvisioningRequest is produced only after polling or initData
// authentication. Its exact serialized bytes are covered by the service token.
type TelegramProvisioningRequest struct {
	BotID int64         `json:"bot_id"`
	User  telegram.User `json:"user"`
}

func (s Signer) TelegramProvisioningToken(botID int64, body []byte) string {
	return s.token(provisioningDigest(botID, body), telegramProvisioningAudience)
}

func (s Signer) VerifyTelegramProvisioning(token string, botID int64, body []byte) error {
	bad := errors.New("invalid provisioning identity")
	if len(body) > MaxProvisioningBytes || len(token) > MaxProvisioningBytes || botID <= 0 || botID >= 1<<52 {
		return bad
	}
	subject, err := s.verify(token, telegramProvisioningAudience)
	if err != nil || subject != provisioningDigest(botID, body) {
		return bad
	}
	encoded, _, _ := strings.Cut(token, ".")
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return bad
	}
	var claims Claims
	if json.Unmarshal(raw, &claims) != nil || claims.Expires > time.Now().Add(time.Minute).Unix() {
		return bad
	}
	return nil
}

func provisioningDigest(botID int64, body []byte) string {
	sum := sha256.Sum256(body)
	return strconv.FormatInt(botID, 10) + ":" + hex.EncodeToString(sum[:])
}
