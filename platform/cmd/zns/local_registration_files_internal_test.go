package main

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestCombinedClientInjectsRegistrationFiles(t *testing.T) {
	t.Parallel()
	filesDB := &pgxpool.Pool{}
	client := combinedClient(
		"http://127.0.0.1:1",
		nil,
		appservices.Services{Orders: orders.Service{DB: filesDB}},
		applicationauth.Authorizer{},
	)
	require.NotNil(t, client.LocalRegistration)
	require.Same(t, filesDB, client.LocalRegistration.Files.DB)
}
