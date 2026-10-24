package miniapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"

	"github.com/google/uuid"
)

const nonceBytes = 32

// SessionKey derives a purpose-separated key from the configured bot token.
// Compare Generation on every persisted-session read, not only on bootstrap.
type SessionKey struct{ key [sha256.Size]byte }

func NewSessionKey(botToken string) SessionKey {
	mac := hmac.New(sha256.New, []byte(botToken))
	_, _ = mac.Write([]byte("zinusya/miniapp/session-key/v1"))
	var key SessionKey
	copy(key.key[:], mac.Sum(nil))
	return key
}

func (k SessionKey) Generation() [sha256.Size]byte {
	return k.digest([]byte("zinusya/miniapp/generation/v1"))
}

func DecodeNonce(encoded string) ([nonceBytes]byte, error) {
	var nonce [nonceBytes]byte
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(raw) != nonceBytes || base64.RawURLEncoding.EncodeToString(raw) != encoded {
		return nonce, ErrInvalid
	}
	copy(nonce[:], raw)
	return nonce, nil
}

// Bearer is deterministic for one persisted attempt, making a lost bootstrap
// reply recoverable without storing the plaintext browser credential.
func (k SessionKey) Bearer(botID int64, sessionID uuid.UUID, nonce [nonceBytes]byte) (string, error) {
	if botID <= 0 || botID > maxTelegramID || sessionID == uuid.Nil {
		return "", ErrInvalid
	}
	input := []byte("zinusya/miniapp/bearer/v1")
	input = binary.BigEndian.AppendUint64(input, uint64(botID))
	input = append(input, sessionID[:]...)
	input = append(input, nonce[:]...)
	digest := k.digest(input)
	return base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

func (k SessionKey) digest(input []byte) [sha256.Size]byte {
	mac := hmac.New(sha256.New, k.key[:])
	_, _ = mac.Write(input)
	var result [sha256.Size]byte
	copy(result[:], mac.Sum(nil))
	return result
}
