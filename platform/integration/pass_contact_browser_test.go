package integration_test

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRegistrationContactBrowser(t *testing.T) {
	t.Parallel()
	if os.Getenv("CONTACT_BROWSER") != "1" {
		t.Skip("set CONTACT_BROWSER=1 and NODE_BINARY for isolated browser test")
	}
	f := setup(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	node := os.Getenv("NODE_BINARY")
	if node == "" {
		node = "node"
	}
	command := exec.CommandContext(ctx, node, "tests/registration-contact.mjs")
	command.Dir = ".."
	command.Env = append(os.Environ(), "SANDBOX_URL="+f.fake.URL)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	t.Log(string(output))
}
