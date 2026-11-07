// Package registrationdetails keeps legal registration data separate from the public profile.
package registrationdetails

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"

	"github.com/google/uuid"
)

var ErrProtection = errors.New("registration details protection failed")

const protectionContext = "zinusya/registration-details/v1/"
const ciphertextVersion byte = 1
const protectionKeySize = 32

// Protector uses a persistent deployment secret. Replacing it prevents old data
// decryption and operation replay; back it up with the database.
type Protector struct {
	aead       cipher.AEAD
	receiptKey []byte
}

func NewProtector(secret []byte) (*Protector, error) {
	if len(secret) != protectionKeySize {
		return nil, ErrProtection
	}
	key, err := hkdf.Key(sha256.New, secret, nil, protectionContext+"encryption", protectionKeySize)
	if err != nil {
		return nil, ErrProtection
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrProtection
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrProtection
	}
	receiptKey, err := hkdf.Key(sha256.New, secret, nil, protectionContext+"receipt", protectionKeySize)
	if err != nil {
		return nil, ErrProtection
	}
	return &Protector{aead: aead, receiptKey: receiptKey}, nil
}

// Seal binds a serialized details record to its owner. Validate the record before
// encryption. Errors never include the submitted data.
func (p *Protector) Seal(owner uuid.UUID, record string) ([]byte, error) {
	if p == nil || p.aead == nil || owner == uuid.Nil {
		return nil, ErrProtection
	}
	nonce := make([]byte, p.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, ErrProtection
	}
	out := append([]byte{ciphertextVersion}, nonce...)
	return p.aead.Seal(out, nonce, []byte(record), ownerContext(owner)), nil
}

func (p *Protector) Open(owner uuid.UUID, encrypted []byte) (string, error) {
	if p == nil || p.aead == nil || owner == uuid.Nil || len(encrypted) < 1+p.aead.NonceSize()+p.aead.Overhead() ||
		encrypted[0] != ciphertextVersion {
		return "", ErrProtection
	}
	size := p.aead.NonceSize()
	value, err := p.aead.Open(nil, encrypted[1:1+size], encrypted[1+size:], ownerContext(owner))
	if err != nil {
		return "", ErrProtection
	}
	return string(value), nil
}

// Fingerprint accepts the canonical normalized mutation, including its operation
// and expected version. Keying avoids a public guessable hash of passport data.
func (p *Protector) Fingerprint(owner uuid.UUID, request []byte) ([]byte, error) {
	if p == nil || len(p.receiptKey) != sha256.Size || owner == uuid.Nil || len(request) == 0 {
		return nil, ErrProtection
	}
	mac := hmac.New(sha256.New, p.receiptKey)
	_, _ = mac.Write(ownerContext(owner))
	_, _ = mac.Write(request)
	return mac.Sum(nil), nil
}

func ownerContext(owner uuid.UUID) []byte {
	return []byte(protectionContext + owner.String())
}
