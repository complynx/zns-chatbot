package integration_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"testing"
	"testing/iotest"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestArchiveStorageCancelledCommitRollsBackWithOnePoolConnection(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	ownerID, _ := registrationUser(t, h, 9801, true)
	owner, err := h.store.GetUser(context.Background(), ownerID)
	require.NoError(t, err)
	parsed, err := url.Parse(h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("pool_max_conns", "1")
	parsed.RawQuery = query.Encode()
	single, err := db.Open(context.Background(), parsed.String())
	require.NoError(t, err)
	t.Cleanup(single.Close)
	scope := db.ProfileScope{BotID: 123, Organization: "org", Subject: owner.Subject, UserID: ownerID}
	raw, digest := receivingArchive(t, 123)
	r := db.LegacyArchiveRequest{OperationID: uuid.New(), Arguments: db.LegacyArchiveArguments{SHA256: digest}}
	prep, err := single.PrepareLegacyArchive(context.Background(), scope, r.OperationID, r.Arguments)
	require.NoError(t, err)
	r.ProposalID = prep.ID
	open := func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
	noopAuthorization := func(context.Context) error { return nil }
	early, earlyCancel := context.WithTimeout(context.Background(), 3*time.Second)
	_, err = single.StoreLegacyArchive(early, scope, r, func(context.Context) (io.ReadCloser, error) {
		earlyCancel()
		return io.NopCloser(bytes.NewReader(raw)), nil
	}, noopAuthorization)
	require.ErrorIs(t, err, context.Canceled)
	earlyCancel()
	_, err = single.StoreLegacyArchive(context.Background(), scope, r, func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(iotest.ErrReader(io.ErrUnexpectedEOF)), nil
	}, noopAuthorization)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = single.StoreLegacyArchive(ctx, scope, r, open, func(context.Context) error { cancel(); return ctx.Err() })
	require.ErrorIs(t, err, context.Canceled)
	retryCtx, retryCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer retryCancel()
	var archives, chunks int
	require.NoError(
		t,
		single.Pool().
			QueryRow(retryCtx, "SELECT (SELECT count(*) FROM shuttle_legacy_archives),(SELECT count(*) F" +
"ROM shuttle_legacy_archive_chunks)").
			Scan(&archives, &chunks),
	)
	require.Zero(t, archives)
	require.Zero(t, chunks)
	receipt, err := single.StoreLegacyArchive(retryCtx, scope, r, open, func(context.Context) error { return nil })
	require.NoError(t, err)
	stored, err := single.LegacyArchiveOperation(retryCtx, scope, r.OperationID)
	require.NoError(t, err)
	require.Equal(t, receipt, stored)
}

func TestArchiveFileExpiredProposalReturnsTerminalRejectionWithoutTelegramRead(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	raw, digest := receivingArchive(t, 123)
	p.fileBytes = raw
	h := setup(t, p)
	_, admin := registrationUser(t, h, 456, true)
	r := db.LegacyArchiveRequest{
		OperationID: uuid.New(),
		Arguments:   db.LegacyArchiveArguments{SHA256: digest, SourceFileID: acceptedArchiveFile(t, h)},
	}
	r.ProposalID = prepareStoredArchive(t, h, admin, r.OperationID, r.Arguments)
	_, err := h.store.Pool().
		Exec(context.Background(), "UPDATE shuttle_legacy_archive_preparations SET expires_at=clock_timestam" +
"p()-interval '1 hour' WHERE id=$1", r.ProposalID)
	require.NoError(t, err)
	code, data := storeArchiveFile(t, h, admin, r)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"status":"rejected"`)
	require.Contains(t, string(data), `"classification":"terminal_rejection"`)
	p.mu.Lock()
	reads := p.fileGets
	p.mu.Unlock()
	require.Zero(t, reads)
}
