package replacement

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type diagnosticCommand struct{ err error }

func (d diagnosticCommand) Run(context.Context, []string, []string) ([]byte, error) {
	return nil, d.err
}

func TestDockerDiagnosticFixedOperationAndDeadline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	d := Docker{Command: diagnosticCommand{err: errors.New("private command arguments")}}
	for _, test := range []struct {
		command string
		stage   observationStage
	}{
		{"ps", "docker_list"}, {"inspect", "docker_inspect"}, {"private argument", "docker_command"},
	} {
		_, err := d.call(ctx, []string{test.command}, nil)
		var failure *observationFailure
		require.ErrorAs(t, err, &failure)
		require.Equal(t, test.stage, failure.stage)
		require.Equal(t, observationPredicate("operation"), failure.predicate)
		require.Equal(t, "deadline_exceeded", failure.deadline)
		require.Equal(t, "replacement observation failed", failure.Error())
		require.NotContains(t, failure.Error(), "private")
		require.GreaterOrEqual(t, failure.elapsed, time.Duration(0))
	}
}

func TestDockerDiagnosticMalformedInventory(t *testing.T) {
	t.Parallel()
	_, err := (Docker{}).decodeInventory([]byte("private invalid json"))
	var failure *observationFailure
	require.ErrorAs(t, err, &failure)
	require.Equal(t, observationStage("docker_decode"), failure.stage)
	require.Equal(t, observationPredicate("invalid_json"), failure.predicate)
	require.ErrorIs(t, err, ErrUnknown)
}
