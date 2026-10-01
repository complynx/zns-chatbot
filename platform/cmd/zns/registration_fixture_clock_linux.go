//go:build linux

package main

import (
	"errors"
	"os"
	"syscall"
)

func registrationClockPlatform() error { return nil }

func registrationClockOwner(file, directory os.FileInfo) error {
	for _, info := range []os.FileInfo{file, directory} {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int64(stat.Uid) != int64(os.Geteuid()) {
			return errors.New("registration clock file and directory must belong to the app UID")
		}
	}
	return nil
}
