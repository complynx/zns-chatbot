package credits

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestCorrelationRejectsUnsafeHeaders(t *testing.T) {
	t.Parallel()
	for _, invalid := range []string{"", strings.Repeat("x", 257), "req\nprivate", "req value", "réquest"} {
		call := &Call{usage: Usage{Basis: basisUnknown}}
		call.CaptureRequestID("req_valid")
		call.CaptureRequestID(invalid)
		call.Capture(Usage{Basis: basisUnknown, ResponseID: "resp_separate"})
		require.Equal(t, "req_valid", call.usage.RequestID)
		require.Equal(t, "resp_separate", call.usage.ResponseID)
	}
}
