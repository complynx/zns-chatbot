package identityprovision

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestServiceAndSDKRequireExplicitLoopbackHTTP(t *testing.T) {
	t.Parallel()
	for _, issuer := range []string{"http://localhost:8113", "http://127.0.0.1:8113", "http://[::1]:8113"} {
		t.Run(issuer, func(t *testing.T) {
			t.Parallel()
			s := Service{
				DB:           &pgxpool.Pool{},
				Provider:     &SDK{},
				Issuer:       issuer,
				Organization: "synthetic",
				BotID:        77,
				EmailDomain:  "telegram.invalid",
			}
			require.ErrorIs(t, s.validate(Telegram{ID: 101}), ErrInvalid)
			s.AllowLocalHTTP = true
			require.NoError(t, s.validate(Telegram{ID: 101}))
			cfg := SDKConfig{
				Issuer:      issuer,
				TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "synthetic"}),
			}
			_, err := NewSDK(cfg)
			require.Error(t, err)
			cfg.AllowLocalHTTP = true
			sdk, err := NewSDK(cfg)
			require.NoError(t, err)
			require.NoError(t, sdk.Close())
		})
	}
}
