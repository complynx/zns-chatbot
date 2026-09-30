package migrate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJSONRejectsLossyUnicodeEscapes(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`"\ud800"`, `"\udfff"`, `"prefix\ud800suffix"`, `"\ud800\ud800"`, `"\udc00\ud800"`, `"\ud800\u0041"`, `"\ud800\\udc00"`, `{"\ud800":"value"}`, `{"content":"\ud800"}`, `["\udfff"]`} {
		t.Run(raw, func(t *testing.T) { t.Parallel(); require.Error(t, validJSON([]byte(raw))) })
	}
}
func TestJSONPreservesValidUnicodeEscapes(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`"�"`, `"\ufffd"`, `"\uFFFD"`, `"\ud83d\ude00"`, `"\uD800\uDC00"`, `"\udbff\udfff"`, `"\\ud800"`, `"prefix\\udfff"`, `{"body":"Ю界😀"}`, `"\\\"\ud83d\ude00"`} {
		t.Run(raw, func(t *testing.T) { t.Parallel(); require.NoError(t, validJSON([]byte(raw))) })
	}
}
