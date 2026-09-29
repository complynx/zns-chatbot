package observability

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLineupDiagnosticExactOperation(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "lineup.query", safeAgentOperation("lineup.query"))
	assert.Equal(t, diagnosticUnknown, safeAgentOperation("lineup.secret"))
	assert.Equal(t, diagnosticUnknown, safeAgentOperation("lineup.query/private-dj"))
}
