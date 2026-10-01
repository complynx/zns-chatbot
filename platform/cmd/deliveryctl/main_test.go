package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
)

func TestDecodeRejectsUntrustedEnvelope(t *testing.T) {
	t.Parallel()
	for _, input := range []string{`{"operation":"a","effect":"b","payload":"private"}`, `{} {}`, strings.Repeat(" ", maxInput+1), `null`} {
		t.Run(input[:min(len(input), 30)], func(t *testing.T) {
			t.Parallel()
			var out botdelivery.IntentKey
			require.Error(t, decode(strings.NewReader(input), &out))
		})
	}
}
