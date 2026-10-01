//go:build linux

package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/complynx/zns-chatbot/platform/internal/registrationclock"
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
	info, err := file.Stat()
	if err != nil {
		return err
	}
	directory := file.Name()
	if !info.IsDir() {
		directory = filepath.Dir(directory)
	}
	root := filepath.Dir(registrationclock.Path)
	if !registrationClockPathWithin(directory, root) {
		return errors.New("registration clock requires its allocated reader mount")
	}
	fdinfo, err := registrationClockMountData("/proc/self/fdinfo/" + strconv.FormatUint(uint64(file.Fd()), 10))
	if err != nil {
		return err
	}
	var mountID string
	for line := range strings.SplitSeq(string(fdinfo), "\n") {
		if value, found := strings.CutPrefix(line, "mnt_id:"); found {
			mountID = strings.TrimSpace(value)
		}
	}
	raw, err := registrationClockMountData("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	return registrationClockReaderMount(raw, mountID, root)
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

const registrationClockMountBytes = 4 << 20

func registrationClockMountData(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, registrationClockMountBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > registrationClockMountBytes {
		return nil, errors.New("registration clock mount metadata too large")
	}
	return raw, nil
}
