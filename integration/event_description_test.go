package integration_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestEventApprovalDescriptionFitsLoomWithoutDroppingArguments(t *testing.T) {
	t.Parallel()
	for _, name := range []string{strings.Repeat("😀", 256), strings.Repeat("\u009f", 256)} {
		t.Run(fmt.Sprintf("name-bytes-%d", len(name)), func(t *testing.T) {
			t.Parallel()
			h, scope, args := eventChangeFixture(t)
			args.Document.Name = name
			args.Document.Modules = nil
			args.Document.Registration.Pricing = &registrations.TierPolicy{Mode: registrations.PricingPaired}
			for i := range 100 {
				args.Document.Modules = append(args.Document.Modules, fmt.Sprintf("m%03d", i)+strings.Repeat("x", 124))
				args.Document.Registration.Pricing.Tiers = append(
					args.Document.Registration.Pricing.Tiers,
					registrations.Tier{
						ID: fmt.Sprintf(
							"%03d",
							i,
						) + strings.Repeat(
							"x",
							61,
						),
						Amount:     1,
						PriceMinor: 12345,
						StartsAt:   args.Document.StartsAt,
					},
				)
			}
			prep, err := h.store.PrepareEventChange(t.Context(), scope, "bounded-create", args)
			require.NoError(t, err)
			require.LessOrEqual(t, len(prep.Description), 4000)
			require.Equal(t, args.Document.Name, prep.Arguments.Document.Name)
			require.Equal(t, args.Document.Modules, prep.Arguments.Document.Modules)
			require.Equal(t, args.Document.Registration.Pricing, prep.Arguments.Document.Registration.Pricing)
			created, err := h.store.ConfirmEventChange(t.Context(), scope, "bounded-create", args, prep.ID)
			require.NoError(t, err)
			update := db.EventChange{ID: args.ID, ExpectedVersion: created.Version, Document: created.Document}
			update.Document.Modules = []string{}
			update.Document.Registration, update.Document.Deposit = nil, nil
			replace, err := h.store.PrepareEventChange(t.Context(), scope, "bounded-replace", update)
			require.NoError(t, err)
			require.LessOrEqual(t, len(replace.Description), 4000)
			require.Contains(t, replace.Description, "removed: 100")
			require.Contains(t, replace.Description, "Registration configured: true -> false")
			require.Contains(t, replace.Description, "deposit configured: true -> false")
			require.Nil(t, replace.Arguments.Document.Registration)
			require.Nil(t, replace.Arguments.Document.Deposit)
		})
	}
}
