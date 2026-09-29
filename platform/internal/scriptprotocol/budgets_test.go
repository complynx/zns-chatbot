package scriptprotocol_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
)

func TestFiniteExecutionBudgets(t *testing.T) {
	t.Parallel()
	require.Equal(t, 200*time.Millisecond, scriptprotocol.ActiveTime)
	require.Equal(t, 10*time.Second, scriptprotocol.HostCallTimeout)
	require.Equal(t, 60*time.Second, scriptprotocol.ExecuteTimeout)
	require.Equal(t, scriptprotocol.ExecuteTimeout+time.Second, scriptprotocol.ExecuteProcessTimeout)
	require.Equal(t, scriptprotocol.ExecuteProcessTimeout+time.Second, scriptprotocol.ExecuteTransportTimeout)
	require.Equal(t, 2*time.Second, scriptprotocol.EvaluateProcessTimeout)
	require.Equal(t, 3*time.Second, scriptprotocol.EvaluateHostTimeout)
	require.Equal(t, 5*time.Second, scriptprotocol.EvaluateClientTimeout)
	require.Equal(t, time.Second, scriptprotocol.ProcessWaitDelay)
}
