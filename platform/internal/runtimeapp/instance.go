package runtimeapp

import (
	"encoding/hex"
	"errors"
	"os"
)

// Instance identifies one deployment generation. It is not domain authorization.
type Instance struct {
	Installation string `json:"installation"`
	Launch       string `json:"launch"`
}

const InstallationEnv = "ZNS_INSTALLATION_ID"
const LaunchEnv = "ZNS_LAUNCH_ID"
const installationBytes = 6
const launchBytes = 12

var ErrInstance = errors.New("invalid runtime instance identity")

// EnvironmentInstance reads the coordinator's identity before any database connection.
func EnvironmentInstance(required bool) (Instance, error) {
	instance := Instance{Installation: os.Getenv(InstallationEnv), Launch: os.Getenv(LaunchEnv)}
	if !required && instance == (Instance{}) {
		return instance, nil
	}
	return instance, instance.Validate()
}

// Validate rejects ambiguous, truncated and noncanonical identifiers.
func (i Instance) Validate() error {
	if !canonicalHex(i.Installation, installationBytes) || !canonicalHex(i.Launch, launchBytes) {
		return ErrInstance
	}
	return nil
}

// ApplicationName is bounded below PostgreSQL's 63-byte application_name limit.
func (i Instance) ApplicationName(component string) (string, error) {
	if err := i.Validate(); err != nil {
		return "", err
	}
	switch component {
	case "app", "admit", "media":
		return "zns:" + i.Installation + ":" + i.Launch + ":" + component, nil
	default:
		return "", ErrInstance
	}
}

func canonicalHex(value string, size int) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == size && hex.EncodeToString(decoded) == value
}
