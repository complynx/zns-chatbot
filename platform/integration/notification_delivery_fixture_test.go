package integration_test

import (
	"context"
	"strings"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

// Production composition is a separate owner; this fixture binds synthetic identity before service copies.
func notificationFixtureServices(db *pgxpool.Pool, options appservices.Options) appservices.Services {
	if options.NativeRegistrationAuthorizer == nil {
		options.NativeRegistrationAuthorizer = fixtureNativeRegistrationAuthorizer(db)
	}
	s := appservices.NewServices(db, options)
	settings := syntheticDeliverySettings()
	s.BotDelivery.Delivery = settings
	s.AdminMessages.Delivery = settings
	s.Orders.Delivery = settings
	s.LegacyOrders.Delivery = settings
	s.Registration.Delivery = settings
	s.Massage.Delivery = settings
	s.LegacyFood.Delivery = settings
	s.DerivedMutations.Orders = s.Orders
	s.DerivedMutations.Registration = s.Registration
	s.DerivedMutations.Massage = s.Massage
	s.DerivedMutations.Food = s.LegacyFood
	return s
}

// Domain-only notice tests cancel prepared work after inspecting it; no synthetic send receipt is invented.
func cancelPassTestNotice(t *testing.T, s passbooking.Service, n passbooking.Notification) error {
	t.Helper()
	return s.CompleteNotification(
		t.Context(),
		passbooking.NotificationCompletion{
			ID:      n.ID,
			Attempt: n.DeliveryAttempt,
			Outcome: delivery.Outcome{Kind: delivery.Cancelled, Reason: "synthetic_domain_notice_consumed"},
		},
	)
}

func deferPassTestNotice(ctx context.Context, s passbooking.Service, n passbooking.Notification) error {
	return s.CompleteNotification(
		ctx,
		passbooking.NotificationCompletion{
			ID:      n.ID,
			Attempt: n.DeliveryAttempt,
			Outcome: delivery.Outcome{
				Kind:    delivery.Deferred,
				Reason:  "notification_preflight_unavailable",
				Missing: true,
			},
		},
	)
}

// Fixture intake uses the same signed-principal boundary and the seeded sender
// binding; it does not replace registration permission/version checks.
func fixtureNativeRegistrationAuthorizer(db *pgxpool.Pool) derivedmutation.NativeRegistrationAuthorizer {
	signer := identity.Signer{Key: []byte(strings.Repeat("n", 32))}
	auth := applicationauth.Authorizer{
		DB:     db,
		Verify: func(_ context.Context, token string) (string, error) { return signer.Verify(token) },
	}
	return func(ctx context.Context, owner string, botID, sender int64) (bool, error) {
		if botID <= 0 {
			return false, nil
		}
		principal, err := auth.Authorize(ctx, signer.Token(owner))
		if err != nil {
			return false, err
		}
		var bound bool
		err = db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM core.users WHERE id=$1 AND telegram_id=$2)", principal.Owner(), sender).
			Scan(&bound)
		return bound, err
	}
}
