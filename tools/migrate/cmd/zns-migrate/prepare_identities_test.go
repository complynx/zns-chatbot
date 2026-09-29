package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIdentityPreparationCLIRejectsSecretArguments(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	assert.Equal(t, 1, run([]string{"prepare-identities", "users", "--token=PRIVATE-secret"}, &output))
	assert.Contains(t, output.String(), "invalid_arguments")
	assert.NotContains(t, output.String(), "PRIVATE")
}
