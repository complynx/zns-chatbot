package replacement

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

const commandTimeout = 10 * time.Second
const commandOutputLimit = 2 << 20
const commandStderrLimit = 8 << 10

// Command runs Docker directly without shell interpretation.
type Command interface {
	Run(context.Context, []string, []string) ([]byte, error)
}

// DockerCommand bounds execution and output; errors never expose command output or secrets.
type DockerCommand struct{}

type commandError struct {
	exitCode     int
	threadDenied bool
	cancellation error
}

func (*commandError) Error() string { return "Docker operation failed" }

func (e *commandError) Unwrap() error { return e.cancellation }

// Run executes only the installed Docker CLI.
func (DockerCommand) Run(ctx context.Context, args, environment []string) ([]byte, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	command.Env = append(os.Environ(), environment...)
	var output boundedOutput
	command.Stdout = &output
	var stderr privateStderr
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if failure, ok := errors.AsType[*exec.ExitError](err); ok {
			result := &commandError{exitCode: failure.ExitCode(), threadDenied: ctx.Err() == nil &&
				failure.ExitCode() == 2 && bytes.Contains(stderr.Bytes(), []byte("runtime: failed to create new OS thread"))}
			if failure.ExitCode() == -1 {
				result.cancellation = ctx.Err()
			}
			return nil, result
		}
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return nil, ctx.Err()
		}
		return nil, errors.New("Docker operation failed")
	}
	return output.Bytes(), nil
}

type boundedOutput struct {
	buffer bytes.Buffer
}

type privateStderr struct {
	buffer bytes.Buffer
}

func (b *boundedOutput) Bytes() []byte { return b.buffer.Bytes() }

func (b *privateStderr) Bytes() []byte { return b.buffer.Bytes() }

// Write drains stderr while retaining only a bounded private prefix.
func (b *privateStderr) Write(data []byte) (int, error) {
	count := len(data)
	retained := min(count, commandStderrLimit-b.buffer.Len())
	_, err := b.buffer.Write(data[:retained])
	return count, err
}

// Write fails before retaining unbounded Docker output.
func (b *boundedOutput) Write(data []byte) (int, error) {
	if b.buffer.Len()+len(data) > commandOutputLimit {
		return 0, ErrUnknown
	}
	return b.buffer.Write(data)
}

// Docker is the sole-launcher adapter for a fixed reviewed Compose topology.
type Docker struct {
	Command      Command
	Installation string
	Project      string
	Files        []string
	ManagedRoles []string
	Database     string
	DatabaseHost string
}

func (d Docker) call(ctx context.Context, args, environment []string) ([]byte, error) {
	start := time.Now()
	limited, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	data, err := d.Command.Run(limited, args, environment)
	if err != nil {
		stage := observationStage("docker_command")
		if len(args) != 0 {
			switch args[0] {
			case "ps":
				stage = "docker_list"
			case "inspect":
				stage = "docker_inspect"
			case "compose":
				operation := len(d.composeArgs())
				if len(args) > operation {
					switch args[operation] {
					case "config":
						stage = "compose_config"
					case "create":
						stage = "compose_create"
					}
				}
			}
		}
		return nil, observationError(limited, start, stage, "operation", err)
	}
	return data, nil
}

// Identity binds the ledger to the Docker daemon.
func (d Docker) Identity(ctx context.Context) (string, error) {
	data, err := d.call(ctx, []string{"info", "--format", "{{.ID}}"}, nil)
	return strings.TrimSpace(string(data)), err
}

// Inventory includes stopped and restarting instances of the installation.
func (d Docker) Inventory(ctx context.Context) ([]Container, error) {
	data, err := d.call(
		ctx,
		[]string{"ps", "-a", "-q", "--no-trunc", "--filter", "label=" + LabelInstallation + "=" + d.Installation},
		nil,
	)
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(data))
	if len(ids) == 0 {
		return nil, nil
	}
	args := append([]string{"inspect", "--type", "container"}, ids...)
	data, err = d.call(ctx, args, nil)
	if err != nil {
		return nil, err
	}
	return d.decodeInventory(data)
}

type dockerContainer struct {
	ID      string `json:"Id"`
	Image   string `json:"Image"`
	Created string `json:"Created"`
	State   struct {
		Status     string `json:"Status"`
		ExitCode   int    `json:"ExitCode"`
		OOMKilled  bool   `json:"OOMKilled"`
		Running    bool   `json:"Running"`
		Restarting bool   `json:"Restarting"`
		Paused     bool   `json:"Paused"`
		PID        int    `json:"Pid"`
		Health     *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	Config struct {
		Labels map[string]string `json:"Labels"`
		Image  string            `json:"Image"`
		Env    []string          `json:"Env"`
		Cmd    []string          `json:"Cmd"`
	} `json:"Config"`
	HostConfig struct {
		Privileged    bool   `json:"Privileged"`
		PidMode       string `json:"PidMode"`
		RestartPolicy struct {
			Name string `json:"Name"`
		} `json:"RestartPolicy"`
	} `json:"HostConfig"`
	Mounts []struct {
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	} `json:"Mounts"`
}

func (d Docker) decodeInventory(data []byte) ([]Container, error) {
	start := time.Now()
	var raw []dockerContainer
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, observationError(context.Background(), start, "docker_decode", "invalid_json", ErrUnknown)
	}
	result := make([]Container, 0, len(raw))
	for _, item := range raw {
		if err := d.validateContainer(item); err != nil {
			failure := observationError(context.Background(), start, "docker_validation", "invalid_container", err)
			failure.containers = len(raw)
			return nil, failure
		}
		health := ""
		if item.State.Health != nil {
			health = item.State.Health.Status
		}
		result = append(result, Container{ID: item.ID, Component: item.Config.Labels[LabelComponent],
			Launch: item.Config.Labels[LabelLaunch], Image: item.Image, Created: item.Created,
			Running: item.State.Running, Restarting: item.State.Restarting, Paused: item.State.Paused,
			PID: item.State.PID, Health: health, Status: item.State.Status,
			ExitCode: item.State.ExitCode, OOMKilled: item.State.OOMKilled})
	}
	return result, nil
}

func (d Docker) validateContainer(item dockerContainer) error {
	labels := item.Config.Labels
	instance := runtimeapp.Instance{Installation: labels[LabelInstallation], Launch: labels[LabelLaunch]}
	if instance.Validate() != nil || instance.Installation != d.Installation ||
		!slices.Contains(
			Components(),
			labels[LabelComponent],
		) || item.ID == "" || item.Image == "" || item.Created == "" {
		return ErrUnknown
	}
	if item.HostConfig.Privileged || item.HostConfig.PidMode != "" ||
		item.HostConfig.RestartPolicy.Name != "no" || !strings.Contains(item.Config.Image, "@sha256:") {
		return ErrUnknown
	}
	for _, mount := range item.Mounts {
		if strings.Contains(mount.Source, "docker.sock") || strings.Contains(mount.Destination, "docker.sock") {
			return ErrUnknown
		}
	}
	required := []string{
		runtimeapp.InstallationEnv + "=" + instance.Installation,
		runtimeapp.LaunchEnv + "=" + instance.Launch,
	}
	for _, entry := range required {
		if !slices.Contains(item.Config.Env, entry) {
			return ErrUnknown
		}
	}
	if labels[LabelComponent] == componentApp && !slices.Equal(item.Config.Cmd, []string{componentApp}) {
		return ErrUnknown
	}
	return d.validateDatabase(item)
}

func (d Docker) composeArgs() []string {
	args := []string{"compose", "--project-name", d.Project}
	for _, file := range d.Files {
		args = append(args, "--file", file)
	}
	return append(args, "--profile", "runtime")
}

// Create prepares, validates and returns exact identities without starting any process.
func (d Docker) Create(ctx context.Context, instance runtimeapp.Instance) ([]Container, error) {
	if instance.Installation != d.Installation || instance.Validate() != nil {
		return nil, ErrConfiguration
	}
	environment := []string{
		runtimeapp.InstallationEnv + "=" + instance.Installation,
		runtimeapp.LaunchEnv + "=" + instance.Launch,
	}
	if err := d.validateComposition(ctx, environment); err != nil {
		return nil, err
	}
	args := append(d.composeArgs(), "create", "--no-build", "--pull", "never")
	args = append(args, Components()...)
	if _, err := d.call(ctx, args, environment); err != nil {
		return nil, err
	}
	inventory, err := d.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	if len(inventory) != len(Components()) {
		return nil, ErrUnknown
	}
	ordered := make([]Container, 0, len(inventory))
	for _, component := range Components() {
		matches := 0
		for _, item := range inventory {
			if item.Component == component && item.Launch == instance.Launch && !item.Running && item.PID == 0 {
				ordered = append(ordered, item)
				matches++
			}
		}
		if matches != 1 {
			return nil, ErrUnknown
		}
	}
	return ordered, nil
}

// Start starts only the exact prepared IDs, helpers before app.
func (d Docker) Start(ctx context.Context, containers []Container) error {
	for _, item := range containers {
		if _, err := d.call(ctx, []string{"start", item.ID}, nil); err != nil {
			return err
		}
		if err := d.waitHealthy(ctx, item.ID); err != nil {
			return err
		}
	}
	return nil
}

// Stop uses the caller's graceful deadline without a daemon SIGKILL fallback.
func (d Docker) Stop(ctx context.Context, containers []Container) error {
	if len(containers) == 0 {
		return nil
	}
	bounded, cancel := context.WithTimeout(ctx, defaultStopTimeout)
	defer cancel()
	args := []string{"stop", "--signal", "SIGTERM", "--timeout", "-1"}
	for _, item := range containers {
		args = append(args, item.ID)
	}
	_, err := d.Command.Run(bounded, args, nil)
	return err
}

// Kill targets only still-running known containers after the stop budget.
func (d Docker) Kill(ctx context.Context, containers []Container) error {
	current, err := d.Inventory(ctx)
	if err != nil {
		return err
	}
	args := []string{"kill"}
	for _, item := range current {
		if !slices.ContainsFunc(containers, func(known Container) bool { return item.ID == known.ID }) {
			return ErrUnknown
		}
		if item.Running || item.Restarting || item.Paused || item.PID != 0 {
			args = append(args, item.ID)
		}
	}
	if len(args) == 1 {
		return nil
	}
	_, err = d.call(ctx, args, nil)
	return err
}

// Remove removes only verified stopped containers, without force or shared volumes.
func (d Docker) Remove(ctx context.Context, containers []Container) error {
	if len(containers) == 0 {
		return nil
	}
	args := []string{"rm"}
	for _, item := range containers {
		args = append(args, item.ID)
	}
	_, err := d.call(ctx, args, nil)
	return err
}

func (d Docker) validateDatabase(item dockerContainer) error {
	component := item.Config.Labels[LabelComponent]
	if component != componentApp && component != "media-broker" {
		return nil
	}
	key := "DATABASE_URL"
	if component == componentApp {
		key = "ZNS_DATABASE__URL"
	}
	value := ""
	matches := 0
	for _, entry := range item.Config.Env {
		if suffix, found := strings.CutPrefix(entry, key+"="); found {
			value = suffix
			matches++
		}
	}
	if matches != 1 || (!strings.HasPrefix(value, "postgres://") && !strings.HasPrefix(value, "postgresql://")) {
		return ErrUnknown
	}
	config, err := pgx.ParseConfig(value)
	if err != nil || config.Database != d.Database || config.Host != d.DatabaseHost ||
		!slices.Contains(d.ManagedRoles, config.User) {
		return ErrUnknown
	}
	return nil
}

type composeService struct {
	DependsOn   map[string]json.RawMessage `json:"depends_on"`
	Links       []string                   `json:"links"`
	VolumesFrom []string                   `json:"volumes_from"`
	NetworkMode string                     `json:"network_mode"`
	PID         string                     `json:"pid"`
	IPC         string                     `json:"ipc"`
}

// validateComposition checks the actual resolved model before create can affect dependencies.
func (d Docker) validateComposition(ctx context.Context, environment []string) error {
	data, err := d.call(ctx, append(d.composeArgs(), "config", "--format", "json"), environment)
	if err != nil {
		return err
	}
	var project struct {
		Services map[string]composeService `json:"services"`
	}
	if err = json.Unmarshal(data, &project); err != nil {
		return observationError(ctx, time.Now(), "compose_validation", "invalid_json", ErrUnknown)
	}
	for _, component := range Components() {
		service, found := project.Services[component]
		if !found || len(service.DependsOn) != 0 || len(service.Links) != 0 || len(service.VolumesFrom) != 0 {
			return observationError(ctx, time.Now(), "compose_validation", "invalid_topology", ErrUnknown)
		}
		for _, namespace := range []string{service.NetworkMode, service.PID, service.IPC} {
			if strings.HasPrefix(namespace, "service:") {
				return observationError(ctx, time.Now(), "compose_validation", "shared_namespace", ErrUnknown)
			}
		}
	}
	return nil
}

func (d Docker) waitHealthy(ctx context.Context, id string) error {
	for {
		inventory, err := d.Inventory(ctx)
		if err != nil {
			return err
		}
		index := slices.IndexFunc(inventory, func(item Container) bool { return item.ID == id })
		if index < 0 || !inventory[index].Running {
			return ErrStopped
		}
		switch inventory[index].Health {
		case "", "healthy":
			return nil
		case "starting":
		default:
			return ErrStopped
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
