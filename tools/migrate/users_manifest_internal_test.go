package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUsersManifestReadRejectsChangedBotUnderVerifiedDigest(t *testing.T) {
	t.Parallel()
	stage := filepath.Join(t.TempDir(), "stage")
	_, _, err := Stage("testdata/synthetic", stage, DefaultLimits())
	require.NoError(t, err)
	root, report, err := verifiedUsersStage(stage, DefaultLimits())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })
	before, err := readVerifiedUsersManifest(root, report, DefaultLimits())
	require.NoError(t, err)
	changed := before
	changed.BotID++
	data, err := json.Marshal(changed)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(stage, "snapshot", manifestName), data, 0600))
	_, _, err = readManifest(root, DefaultLimits())
	require.NoError(t, err, "changed manifest remains structurally valid")
	after, err := readVerifiedUsersManifest(root, report, DefaultLimits())
	require.EqualError(t, err, "users_manifest_changed")
	assert.Zero(t, after.BotID, "no changed selection reaches conversion")
}
