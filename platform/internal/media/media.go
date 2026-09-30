// Package media stores private neutral attachments before their purpose is known.
package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

const MaxMediaBytes = 20 << 20
const maxFilenameBytes = 255

type Attachment struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	MIME      string    `json:"mime"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Body      []byte    `json:"-"`
}

type Service struct{ DB *pgxpool.Pool }

// Upload preserves original bytes and detects MIME without trusting the filename.
// An identical owner/name/body retry keeps its ID and creation time, refreshing
// expiry to 24 hours after this upload. A pruned upload can be created again.
func (s Service) Upload(ctx context.Context, owner, filename string, body []byte) (Attachment, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" || filename == "." || filename == ".." || len(filename) > maxFilenameBytes ||
		!utf8.ValidString(filename) || strings.ContainsAny(filename, "/\\") ||
		strings.ContainsFunc(filename, unicode.IsControl) || len(body) == 0 || len(body) > MaxMediaBytes {
		return Attachment{}, &core.ProblemError{Status: http.StatusBadRequest, Code: "invalid_media"}
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(owner + "\x00" + filename + "\x00"))
	_, _ = digest.Write(body)
	attachment := Attachment{ID: hex.EncodeToString(digest.Sum(nil)), Filename: filename,
		MIME: http.DetectContentType(body), Size: int64(len(body))}
	err := s.DB.QueryRow(ctx, `INSERT INTO core.media(id,owner,filename,mime,body)
 SELECT $1,id,$3,$4,$5 FROM core.users WHERE id=$2 AND can_book
 ON CONFLICT (id) DO UPDATE SET expires_at=now()+interval '24 hours'
 RETURNING created_at,expires_at`, attachment.ID, owner, filename, attachment.MIME, body).
		Scan(&attachment.CreatedAt, &attachment.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attachment{}, &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"}
	}
	return attachment, err
}

// Get exposes only unexpired bytes owned by a currently permitted user.
// Missing, foreign, expired and denied reads share one response.
func (s Service) Get(ctx context.Context, owner, id string) (Attachment, error) {
	var attachment Attachment
	err := s.DB.QueryRow(ctx, `SELECT m.id,m.filename,m.mime,octet_length(m.body),m.created_at,m.expires_at,m.body
 FROM core.media m JOIN core.users u ON u.id=m.owner
 WHERE m.id=$1 AND m.owner=$2 AND m.expires_at>now() AND u.can_book`, id, owner).
		Scan(&attachment.ID, &attachment.Filename, &attachment.MIME, &attachment.Size,
			&attachment.CreatedAt, &attachment.ExpiresAt, &attachment.Body)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attachment{}, &core.ProblemError{Status: http.StatusNotFound, Code: "media_not_found"}
	}
	return attachment, err
}

// PromoteProof copies original bytes into separate proof storage. It does not
// attach evidence to an order or approve a payment; orders.Execute does that.
func (s Service) PromoteProof(ctx context.Context, owner, id string) (orders.Proof, error) {
	attachment, err := s.Get(ctx, owner, id)
	if err != nil {
		return orders.Proof{}, err
	}
	return (orders.Service{DB: s.DB}).UploadProof(ctx, owner, attachment.Filename, attachment.Body)
}

// PruneExpired removes at most 100 abandoned attachments per call. Proof copies
// have independent storage and survive cleanup. Concurrent cleaners skip locks.
func (s Service) PruneExpired(ctx context.Context) (int64, error) {
	result, err := s.DB.Exec(ctx, `DELETE FROM core.media WHERE id IN
 (SELECT id FROM core.media WHERE expires_at<=now() ORDER BY expires_at LIMIT 100 FOR UPDATE SKIP LOCKED)`)
	return result.RowsAffected(), core.DatabaseOperationError(err)
}
