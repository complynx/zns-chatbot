package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMassageCommandsValidatePrivateArguments(t *testing.T) {
	t.Parallel()
	for _, verb := range []string{"plan", "apply", "reconcile"} {
		var output bytes.Buffer
		assert.Equal(t, 1, run([]string{verb, "massage", "--unknown=private-canary"}, &output))
		assert.Contains(t, output.String(), "invalid_arguments")
		assert.NotContains(t, output.String(), "private-canary")
	}
}
