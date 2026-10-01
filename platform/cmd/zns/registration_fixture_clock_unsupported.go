//go:build !linux

package main

import (
	"errors"
	"os"
)

func registrationClockPlatform() error {
	return errors.New("configured registration stand clock requires Linux UID guards")
}

func registrationClockOwner(os.FileInfo, os.FileInfo) error { return registrationClockPlatform() }

func registrationClockOpenDirectory(path string) (*os.File, error) { return os.Open(path) }

func registrationClockOpenPublication(_ *os.File, path string) (*os.File, error) {
	return os.Open(path)
}

func registrationClockReadOnly(*os.File) error { return registrationClockPlatform() }
