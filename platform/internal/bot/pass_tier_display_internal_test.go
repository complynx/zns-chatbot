package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassTierMessages(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			now := time.Now()
			tiers := []passallocation.Tier{{Amount: 10, Price: 100, Start: now.Add(time.Hour), BlockedByDate: true}}
			report := passallocation.DescribeTiers(
				tiers,
				passallocation.TierRequest{Rule: passallocation.Distributed, Increment: 1, Now: now},
			)
			status := passbooking.TierStatus{
				Event:       "dance",
				Rule:        passallocation.Distributed,
				Tiers:       tiers,
				Distributed: report,
			}
			messages, err := passTierMessages(language, status)
			require.NoError(t, err)
			assert.Len(t, messages, 5)
			assert.NotContains(t, strings.Join(messages, "\n"), "{")
			assert.Contains(t, messages[0], "0.0%")
			assert.Contains(t, messages[2], "100")
			assert.Contains(t, messages[3], "MSK")
			status.Tiers = nil
			messages, err = passTierMessages(language, status)
			require.NoError(t, err)
			require.Len(t, messages, 1)
			assert.Contains(t, messages[0], "dance")
		})
	}
}
