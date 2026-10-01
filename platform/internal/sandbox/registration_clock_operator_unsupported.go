//go:build !linux

package sandbox

import (
	"errors"
	"os"
)

func registrationClockOperatorPlatform() error {
	return errors.New("registration clock operator requires Linux UID and file-lock guards")
}

func openRegistrationClockState(*os.File, string) (*os.File, error) {
	return nil, errors.New("registration clock operator requires Linux UID and file-lock guards")
}

func lockRegistrationClockDirectory(string) (*os.File, *os.File, error) {
	return nil, nil, registrationClockOperatorPlatform()
}

func openRegistrationClockDirectory(string) (*os.File, error) {
	return nil, registrationClockOperatorPlatform()
}

func validateRegistrationClockDirectory(*os.File, string) error {
	return registrationClockOperatorPlatform()
}

func syncRegistrationClockDirectory(*os.File) error {
	return errors.New("registration clock operator requires Linux directory durability")
}
