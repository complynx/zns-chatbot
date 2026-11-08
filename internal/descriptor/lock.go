package descriptor

import (
	"errors"
	"io"
	"os"
)

const lockProtocol = "zinusya-os-lock-v1\n"

// journalLock keeps its inode in place: unlinking could admit a second owner.
// An unmarked legacy O_EXCL lock requires offline operator reconciliation.
//
//nolint:gosec // The path is inside the operator-selected private journal, never a request path.
func journalLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	created := err == nil
	if errors.Is(err, os.ErrExist) {
		f, err = os.OpenFile(path, os.O_RDWR, 0600)
	}
	if err != nil {
		return nil, err
	}
	if err = lockFile(f); err != nil {
		_ = f.Close()
		return nil, errors.New("descriptor journal already owned")
	}
	if created {
		_, err = f.WriteString(lockProtocol)
		if err == nil {
			err = f.Sync()
		}
	} else {
		var raw []byte
		raw, err = io.ReadAll(io.LimitReader(f, int64(len(lockProtocol)+1)))
		if err == nil && string(raw) != lockProtocol {
			err = errors.New("legacy or incomplete descriptor lock requires offline reconciliation")
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}
