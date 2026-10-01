//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func registrationClockPlatform() error { return nil }

func registrationClockOpenDirectory(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func registrationClockOpenPublication(directory *os.File, path string) (*os.File, error) {
	fd, err := syscall.Openat(
		int(directory.Fd()),
		filepath.Base(path),
		syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

// Both directory replacement and in-place writes must be unavailable through
// the reader mount. The trusted publisher uses a separate writable mount.
func registrationClockReadOnly(file *os.File) error {
	var stat syscall.Statfs_t
	if err := syscall.Fstatfs(int(file.Fd()), &stat); err != nil {
		return err
	}
	const readOnly = 1
	if stat.Flags&readOnly == 0 {
		return errors.New("registration clock requires a read-only reader mount")
	}
	return nil
}

func registrationClockOwner(file, directory os.FileInfo) error {
	for _, info := range []os.FileInfo{file, directory} {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int64(stat.Uid) != int64(os.Geteuid()) {
			return errors.New("registration clock file and directory must belong to the app UID")
		}
	}
	return nil
}
