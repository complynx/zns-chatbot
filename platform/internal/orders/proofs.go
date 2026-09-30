package orders

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"unicode"

	"github.com/complynx/zns-chatbot/platform/internal/core"

	"github.com/jackc/pgx/v5"
)

const MaxProofBytes = 20 << 20
const maxFilenameBytes = 255

type Proof struct {
	Version  int64  `json:"version,omitempty"`
	Attempt  string `json:"attempt,omitempty"`
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Body     []byte `json:"-"`
}

// UploadProof stores immutable bytes. Identical owner, name and bytes reuse an ID.
func (s Service) UploadProof(ctx context.Context, owner, filename string, body []byte) (Proof, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" || len(filename) > maxFilenameBytes || strings.ContainsAny(filename, "/\\") ||
		strings.ContainsFunc(filename, unicode.IsControl) || len(body) == 0 || len(body) > MaxProofBytes {
		return Proof{}, problem(http.StatusBadRequest, "invalid_proof")
	}
	var allowed bool
	err := core.DatabaseOperationError(
		s.DB.QueryRow(ctx, `SELECT can_book FROM core.users WHERE id=$1`, owner).Scan(&allowed),
	)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !allowed) {
		return Proof{}, problem(http.StatusForbidden, "forbidden")
	}
	if err != nil {
		return Proof{}, err
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(owner + "\x00" + filename + "\x00"))
	_, _ = digest.Write(body)
	proof := Proof{ID: hex.EncodeToString(digest.Sum(nil)), Filename: filename}
	_, err = s.DB.Exec(
		ctx,
		`INSERT INTO core.order_proofs(id,owner,filename,body) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
		proof.ID,
		owner,
		filename,
		body,
	)
	return proof, core.DatabaseOperationError(err)
}

// OrderProof authorizes against the current order, never a client-supplied file ID.
func (s Service) OrderProof(ctx context.Context, actor, event, id string) (Proof, error) {
	var proof Proof
	err := s.DB.QueryRow(ctx, `SELECT p.id,p.filename,p.body,o.version,o.attempt `+orderProofScope, actor, event, id).
		Scan(&proof.ID, &proof.Filename, &proof.Body, &proof.Version, &proof.Attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Proof{}, problem(http.StatusNotFound, "proof_not_found")
	}
	return proof, core.DatabaseOperationError(err)
}

const orderProofScope = `FROM core.orders o
 JOIN core.order_proofs p ON p.id=o.proof_file AND p.owner=o.owner
 JOIN core.users u ON u.id=$1
 WHERE o.id=$3 AND o.event_id=$2 AND o.state IN ('proof','paid')
 AND (o.owner=$1 OR (u.can_book AND EXISTS(SELECT 1 FROM core.order_admins a WHERE a.event_id=o.event_id AND a.owner=$1)))`

// OrderProofMetadata checks current access and the immutable file binding without reading its bytes.
func (s Service) OrderProofMetadata(ctx context.Context, actor, event, id string) (Proof, error) {
	var proof Proof
	err := s.DB.QueryRow(ctx, `SELECT p.id,p.filename,o.version,o.attempt `+orderProofScope, actor, event, id).
		Scan(&proof.ID, &proof.Filename, &proof.Version, &proof.Attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Proof{}, problem(http.StatusNotFound, "proof_not_found")
	}
	return proof, core.DatabaseOperationError(err)
}
