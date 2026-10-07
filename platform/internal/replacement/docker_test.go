package replacement_test

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/replacement"
)

type dockerFixture struct {
	document []byte
	calls    [][]string
	deadline time.Time
	entered  time.Time
	err      error
}

func (f *dockerFixture) Run(ctx context.Context, args, _ []string) ([]byte, error) {
	f.calls = append(f.calls, args)
	f.entered = time.Now()
	f.deadline, _ = ctx.Deadline()
	if f.err != nil {
		return nil, f.err
	}
	if args[0] == "ps" {
		return []byte("exact-container-id"), nil
	}
	return f.document, nil
}

func TestDockerStopUsesBoundedGracefulSignalWithoutForcedFallback(t *testing.T) {
	t.Parallel()
	for _, timeout := range []time.Duration{time.Minute, 100 * time.Millisecond} {
		t.Run(timeout.String(), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), timeout)
			defer cancel()
			command := &dockerFixture{err: context.DeadlineExceeded}
			docker := replacement.Docker{Command: command}
			require.ErrorIs(
				t,
				docker.Stop(ctx, []replacement.Container{{ID: "owned-one"}, {ID: "owned-two"}}),
				context.DeadlineExceeded,
			)
			require.Equal(
				t,
				[][]string{{"stop", "--signal", "SIGTERM", "--timeout", "-1", "owned-one", "owned-two"}},
				command.calls,
			)
			require.False(t, command.deadline.IsZero())
			require.LessOrEqual(t, command.deadline.Sub(command.entered), 5*time.Second)
			callerDeadline, _ := ctx.Deadline()
			require.False(t, command.deadline.After(callerDeadline), "stop must not extend the caller's window")
		})
	}
}

func TestDockerInventoryRejectsUnownedExecutionTopology(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"valid", "restart", "privileged", "namespace", "tag", "mutable-image", "socket"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			raw := map[string]any{
				"Id":      "exact-container-id",
				"Image":   "sha256:digest",
				"Created": "timestamp",
				"State":   map[string]any{"Running": false, "Pid": 0},
				"Config": map[string]any{
					"Image": "reviewed@sha256:digest",
					"Cmd":   []string{"app"},
					"Env": []string{
						"ZNS_INSTALLATION_ID=" + installation,
						"ZNS_LAUNCH_ID=" + oldLaunch,
						"ZNS_DATABASE__URL=postgres://runtime@postgres/zns",
					},
					"Labels": map[string]string{
						replacement.LabelInstallation: installation,
						replacement.LabelLaunch:       oldLaunch,
						replacement.LabelComponent:    "app",
					},
				},
				"HostConfig": map[string]any{
					"Privileged":    false,
					"PidMode":       "",
					"RestartPolicy": map[string]string{"Name": "no"},
				},
			}
			host := raw["HostConfig"].(map[string]any)
			config := raw["Config"].(map[string]any)
			switch change {
			case "valid":
			case "restart":
				host["RestartPolicy"] = map[string]string{"Name": "unless-stopped"}
			case "privileged":
				host["Privileged"] = true
			case "namespace":
				host["PidMode"] = "host"
			case "tag":
				config["Env"] = []string{"ZNS_INSTALLATION_ID=other"}
			case "mutable-image":
				config["Image"] = "reviewed:latest"
			case "socket":
				raw["Mounts"] = []map[string]string{{"Source": "/var/run/docker.sock", "Destination": "/docker.sock"}}
			}
			data, err := json.Marshal([]any{raw})
			require.NoError(t, err)
			command := &dockerFixture{document: data}
			docker := replacement.Docker{
				Command:      command,
				Installation: installation,
				ManagedRoles: []string{"runtime"},
				Database:     "zns",
				DatabaseHost: "postgres",
			}
			result, err := docker.Inventory(t.Context())
			if change == "valid" {
				require.NoError(t, err)
				require.Len(t, result, 1)
			} else {
				require.ErrorIs(t, err, replacement.ErrUnknown)
			}
			for _, call := range command.calls {
				require.NotContains(t, strings.Join(call, " "), "kill")
			}
		})
	}
}

func TestDockerKillBatchesOnlyKnownLiveIdentities(t *testing.T) {
	t.Parallel()
	for _, unknown := range []bool{false, true} {
		t.Run(strconv.FormatBool(unknown), func(t *testing.T) {
			t.Parallel()
			var raw []map[string]any
			for index, id := range []string{"live-one", "live-two", "stopped"} {
				raw = append(raw, map[string]any{
					"Id": id, "Image": "sha256:digest", "Created": "timestamp",
					"State":      map[string]any{"Running": index < 2},
					"HostConfig": map[string]any{"RestartPolicy": map[string]string{"Name": "no"}},
					"Config": map[string]any{
						"Image": "reviewed@sha256:digest",
						"Env":   []string{"ZNS_INSTALLATION_ID=" + installation, "ZNS_LAUNCH_ID=" + oldLaunch},
						"Labels": map[string]string{
							replacement.LabelInstallation: installation,
							replacement.LabelLaunch:       oldLaunch,
							replacement.LabelComponent:    "evaluator",
						},
					},
				})
			}
			data, err := json.Marshal(raw)
			require.NoError(t, err)
			command := &dockerFixture{document: data}
			docker := replacement.Docker{Command: command, Installation: installation}
			known := []replacement.Container{{ID: "live-one"}, {ID: "live-two"}, {ID: "stopped"}}
			if unknown {
				known = known[:1]
			}
			err = docker.Kill(t.Context(), known)
			if unknown {
				require.ErrorIs(t, err, replacement.ErrUnknown)
				require.Len(t, command.calls, 2, "unknown identity must prevent any kill")
				return
			}
			require.NoError(t, err)
			require.Len(t, command.calls, 3)
			require.Equal(t, []string{"kill", "live-one", "live-two"}, command.calls[2])
		})
	}
}
