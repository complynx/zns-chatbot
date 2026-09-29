package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/media"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestMediaOwnershipReplayAndExpiry(t *testing.T) {
	t.Parallel()
	db := database(t)
	service := media.Service{DB: db}
	ctx := t.Context()
	body := []byte("%PDF-1.4 original neutral bytes")
	attachment, err := service.Upload(ctx, "alice", "portrait.jpg", body)
	require.NoError(t, err)
	assert.Equal(t, "application/pdf", attachment.MIME)
	assert.EqualValues(t, len(body), attachment.Size)
	assert.WithinDuration(t, attachment.CreatedAt.Add(24*time.Hour), attachment.ExpiresAt, time.Second)
	retry, err := service.Upload(ctx, "alice", attachment.Filename, body)
	require.NoError(t, err)
	assert.Equal(t, attachment.ID, retry.ID)
	assert.Equal(t, attachment.CreatedAt, retry.CreatedAt)
	assert.False(t, retry.ExpiresAt.Before(attachment.ExpiresAt))
	got, err := service.Get(ctx, "alice", attachment.ID)
	require.NoError(t, err)
	assert.Equal(t, body, got.Body)
	_, err = service.Get(ctx, "bob", attachment.ID)
	requireCode(t, err, "media_not_found")
	_, err = service.Get(ctx, "bob", "missing")
	requireCode(t, err, "media_not_found")
	_, err = service.PromoteProof(ctx, "bob", attachment.ID)
	requireCode(t, err, "media_not_found")
	proof, err := service.PromoteProof(ctx, "alice", attachment.ID)
	require.NoError(t, err)
	proofRetry, err := service.PromoteProof(ctx, "alice", attachment.ID)
	require.NoError(t, err)
	assert.Equal(t, proof.ID, proofRetry.ID)
	_, err = db.Exec(ctx, `UPDATE core.media SET expires_at=now()-interval '1 second' WHERE id=$1`, attachment.ID)
	require.NoError(t, err)
	_, err = service.Get(ctx, "alice", attachment.ID)
	requireCode(t, err, "media_not_found")
	_, err = service.PromoteProof(ctx, "alice", attachment.ID)
	requireCode(t, err, "media_not_found")
	count, err := service.PruneExpired(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)
	var stored []byte
	require.NoError(t, db.QueryRow(ctx, `SELECT body FROM core.order_proofs WHERE id=$1`, proof.ID).Scan(&stored))
	assert.Equal(t, body, stored)
	var orderCount int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM core.orders WHERE owner='alice'`).Scan(&orderCount))
	assert.Zero(t, orderCount, "promotion alone must not create or pay an order")
}

func TestMediaValidationAndPermission(t *testing.T) {
	t.Parallel()
	db := database(t)
	service := media.Service{DB: db}
	ctx := t.Context()
	for _, filename := range []string{"", ".", "..", "../a", "a\\b", "bad\nname", string([]byte{255})} {
		_, err := service.Upload(ctx, "alice", filename, []byte("x"))
		requireCode(t, err, "invalid_media")
	}
	for _, body := range [][]byte{nil, make([]byte, media.MaxMediaBytes+1)} {
		_, err := service.Upload(ctx, "alice", "file", body)
		requireCode(t, err, "invalid_media")
	}
	_, err := service.Upload(ctx, "visitor", "file", []byte("x"))
	requireCode(t, err, "forbidden")
	attachment, err := service.Upload(ctx, "alice", "file", []byte{0, 255, 1, 2})
	require.NoError(t, err, "arbitrary bounded bytes are stored neutrally")
	_, err = db.Exec(ctx, `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	_, err = service.Get(ctx, "alice", attachment.ID)
	requireCode(t, err, "media_not_found")
	_, err = service.PromoteProof(ctx, "alice", attachment.ID)
	requireCode(t, err, "media_not_found")
	_, err = service.Upload(ctx, "alice", "file", []byte{0, 255, 1, 2})
	requireCode(t, err, "forbidden")
}

func TestMediaCleanupIsBounded(t *testing.T) {
	t.Parallel()
	db := database(t)
	service := media.Service{DB: db}
	ctx := t.Context()
	_, err := db.Exec(ctx, `INSERT INTO core.media(id,owner,filename,mime,body,expires_at)
 SELECT 'expired-'||n,'alice','file','text/plain',decode('01','hex'),now()-interval '1 second'
 FROM generate_series(1,101) n`)
	require.NoError(t, err)
	active, err := service.Upload(ctx, "alice", "active", []byte("keep"))
	require.NoError(t, err)
	count, err := service.PruneExpired(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 100, count)
	count, err = service.PruneExpired(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)
	got, err := service.Get(ctx, "alice", active.ID)
	require.NoError(t, err)
	assert.Equal(t, []byte("keep"), got.Body)
}

func TestMediaAPIOriginalBytesAndMetadata(t *testing.T) {
	t.Parallel()
	f := setup(t)
	body := []byte("original private attachment")
	response := mediaRequest(t, f, "alice", http.MethodPost, "/v1/media?filename=photo.png", body)
	require.Equal(t, http.StatusOK, response.StatusCode)
	var attachment media.Attachment
	require.NoError(t, json.NewDecoder(response.Body).Decode(&attachment))
	require.NoError(t, response.Body.Close())
	response = mediaRequest(t, f, "alice", http.MethodGet, "/v1/media/"+attachment.ID, nil)
	var metadata map[string]any
	require.NoError(t, json.NewDecoder(response.Body).Decode(&metadata))
	require.NoError(t, response.Body.Close())
	assert.NotContains(t, metadata, "body")
	response = mediaRequest(t, f, "alice", http.MethodGet, "/v1/media/"+attachment.ID+"/file", nil)
	require.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, "no-store", response.Header.Get("Cache-Control"))
	assert.Equal(t, "nosniff", response.Header.Get("X-Content-Type-Options"))
	downloaded, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, body, downloaded)
	response = mediaRequest(t, f, "bob", http.MethodGet, "/v1/media/"+attachment.ID+"/file", nil)
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
	require.NoError(t, response.Body.Close())
	response = mediaRequest(t, f, "alice", http.MethodPost, "/v1/media/"+attachment.ID+"/proof", nil)
	require.Equal(t, http.StatusOK, response.StatusCode)
	var proof orders.Proof
	require.NoError(t, json.NewDecoder(response.Body).Decode(&proof))
	require.NoError(t, response.Body.Close())
	assert.NotEmpty(t, proof.ID)
}

func mediaRequest(t *testing.T, f *fixture, owner, method, path string, body []byte) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, f.b.API.Base+path, bytes.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+f.b.API.Signer.Token(owner))
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	return response
}
