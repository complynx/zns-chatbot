package stickermedia_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/stickermedia"
)

func TestTGSMetadataUsesExactKeys(t *testing.T) {
	t.Parallel()
	for _, replacement := range []struct{ original, replacement string }{
		{`"fr":30`, `"fr":1,"FR":60`},
		{`"w":64`, `"w":2048,"W":64`},
		{`"h":64`, `"h":2048,"H":64`},
		{`"ip":0`, `"ip":-1,"IP":0`},
		{`"op":60`, `"op":180,"OP":60`},
		{`"w":64`, `"W":64`},
	} {
		body := strings.Replace(vectorJSON, replacement.original, replacement.replacement, 1)
		normalizer := stickermedia.Normalizer{TempDir: t.TempDir()}
		_, err := normalizer.Normalize(t.Context(), zipped(t, body), stickermedia.TGS)
		require.ErrorIs(t, err, stickermedia.ErrInvalid, replacement.replacement)
	}
}
