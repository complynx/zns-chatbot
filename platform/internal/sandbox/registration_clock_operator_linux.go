//go:build linux

package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const clockDirectoryPermission = 0o700

func registrationClockOperatorPlatform() error {
	if os.Geteuid() == 0 {
		return errors.New("registration clock operator requires the nonroot app UID")
	}
	return nil
}

func privateClockOwner(info os.FileInfo, directory bool) error {
	if err := registrationClockOperatorPlatform(); err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	mode := os.FileMode(clockFilePermission)
	if directory {
		mode = clockDirectoryPermission
	}
	if !ok || stat.Nlink != 1 && !directory || int64(stat.Uid) != int64(os.Geteuid()) || info.Mode().Perm() != mode ||
		(directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		return errors.New("clock directory and files must be private and owned by the app UID")
	}
	return nil
}

func openPrivateClock(directory *os.File, name string, flags int) (*os.File, error) {
	fd, err := syscall.Openat(int(directory.Fd()), name,
		flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, clockFilePermission)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	file := os.NewFile(uintptr(fd), name)
	info, err := file.Stat()
	if err == nil {
		err = privateClockOwner(info, false)
	}
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func openRegistrationClockState(directory *os.File, name string) (*os.File, error) {
	return openPrivateClock(directory, name, syscall.O_RDONLY)
}

func clockDirectoryMetadata(path string) (os.FileInfo, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("registration clock directory must be absolute")
	}
	root, err := os.OpenRoot("/")
	if err != nil {
		return nil, err
	}
	defer root.Close()
	var info os.FileInfo
	current := "."
	for part := range strings.SplitSeq(strings.TrimPrefix(filepath.Clean(path), "/"), "/") {
		current = filepath.Join(current, part)
		info, err = root.Lstat(current)
		if err != nil || !info.IsDir() {
			return nil, errors.New("registration clock directory path must not contain links")
		}
	}
	if err = privateClockOwner(info, true); err != nil {
		return nil, err
	}
	return info, nil
}

func openRegistrationClockDirectory(path string) (*os.File, error) {
	info, err := clockDirectoryMetadata(path)
	if err != nil {
		return nil, err
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.Join(errors.New("registration clock directory changed during open"), file.Close())
	}
	if err = privateClockOwner(opened, true); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func validateRegistrationClockDirectory(directory *os.File, path string) error {
	visible, err := clockDirectoryMetadata(path)
	if err != nil {
		return err
	}
	pinned, err := directory.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(visible, pinned) {
		return errors.New("registration clock directory binding changed")
	}
	return privateClockOwner(pinned, true)
}

// Keep both directory and lock descriptors alive through read/publication/fsync.
func lockRegistrationClockDirectory(path string) (*os.File, *os.File, error) {
	directory, err := openRegistrationClockDirectory(path)
	if err != nil {
		return nil, nil, err
	}
	var stat syscall.Statfs_t
	if err = syscall.Fstatfs(int(directory.Fd()), &stat); err != nil || stat.Flags&1 != 0 {
		return nil, nil, errors.Join(
			errors.New("registration clock operator requires a writable mount"),
			directory.Close(),
		)
	}
	lock, err := openPrivateClock(directory, "operator.lock", syscall.O_CREAT|syscall.O_RDWR)
	if err != nil {
		return nil, nil, errors.Join(err, directory.Close())
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, nil, errors.Join(
			errors.New("registration clock operator already active"),
			lock.Close(),
			directory.Close(),
		)
	}
	if err = validateRegistrationClockDirectory(directory, path); err != nil {
		return nil, nil, errors.Join(err, lock.Close(), directory.Close())
	}
	return directory, lock, nil
}

func syncRegistrationClockDirectory(directory *os.File) error {
	return directory.Sync()
}
