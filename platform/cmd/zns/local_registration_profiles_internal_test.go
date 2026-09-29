package main

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

func TestCombinedClientInjectsRegistrationProfile(t *testing.T) {
	t.Parallel()
	profileDB := &pgxpool.Pool{}
	client := combinedClient(
		"http://127.0.0.1:1",
		nil,
		appservices.Services{PassProfiles: passes.Service{DB: profileDB}},
		applicationauth.Authorizer{},
	)
	require.NotNil(t, client.LocalRegistration)
	require.Same(t, profileDB, client.LocalRegistration.Profile.DB)
}
