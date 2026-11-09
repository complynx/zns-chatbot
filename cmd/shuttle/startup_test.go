package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

type startupProbe struct {
	calls []string
	fail  string
	botID int64
}

func (p *startupProbe) GetMe(context.Context) (telegram.User, error) {
	p.calls = append(p.calls, "telegram")
	if p.fail == "telegram" {
		return telegram.User{}, errors.New("rejected")
	}
	return telegram.User{ID: p.botID, IsBot: true}, nil
}

func (p *startupProbe) CheckAccess(context.Context) (identity.Credential, error) {
	p.calls = append(p.calls, "iam")
	if p.fail == "iam" {
		return identity.Credential{}, errors.New("IAM credentials rejected")
	}
	return identity.Credential{Token: "service-only"}, nil
}

func (p *startupProbe) CheckModels(_ context.Context, token string) error {
	p.calls = append(p.calls, "loom")
	if p.fail == "loom" || token != "service-only" {
		return errors.New("rejected")
	}
	return nil
}

func TestStartupDependenciesFailBeforeLaterChecks(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"telegram", "iam", "loom", ""} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			p := &startupProbe{fail: failure, botID: 123}
			err := checkStartupDependencies(t.Context(), p.botID, p, p, p)
			if failure == "" {
				require.NoError(t, err)
				require.Equal(t, []string{"telegram", "iam", "loom"}, p.calls)
			} else {
				require.Error(t, err)
				require.Equal(t, failure, p.calls[len(p.calls)-1])
			}
		})
	}
	p := &startupProbe{botID: 456}
	require.ErrorContains(t, checkStartupDependencies(t.Context(), 123, p, p, p), "identity mismatch")
	require.Equal(t, []string{"telegram"}, p.calls)
}
