package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestBanCommandRejectsInvalidTargetWithoutReceipt(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	err := runBanTelegramUser(t.Context(), &db.Store{}, 1, 0, "operator:test", "test", "", &output)
	require.ErrorIs(t, err, db.ErrUserBanInvalid)
	require.Empty(t, output.String())
}
