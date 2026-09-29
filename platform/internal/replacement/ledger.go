package replacement

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// FileJournal stores a single bounded ledger in a protected deployment directory.
type FileJournal struct{ Directory string }

// Load never treats malformed or partially written state as an empty installation.
func (j FileJournal) Load() (Ledger, error) {
	file, err := os.Open(filepath.Join(j.Directory, "ledger.json"))
	if errors.Is(err, os.ErrNotExist) {
		return Ledger{}, nil
	}
	if err != nil {
		return Ledger{}, ErrUnknown
	}
	defer file.Close()
	var ledger Ledger
	decoder := json.NewDecoder(io.LimitReader(file, commandOutputLimit))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&ledger); err != nil {
		return Ledger{}, ErrUnknown
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return Ledger{}, ErrUnknown
	}
	if ledger.Version != 1 || ledger.Installation == "" || ledger.Host == "" || ledger.Daemon == "" {
		return Ledger{}, ErrUnknown
	}
	switch ledger.State {
	case StateStarting, StateRunning, StateStopping, StateStopped, StateBlocked:
		return ledger, nil
	default:
		return Ledger{}, ErrUnknown
	}
}

// Save fsyncs the new file and containing directory before returning.
func (j FileJournal) Save(ledger Ledger) (result error) {
	data, err := json.Marshal(ledger)
	if err != nil {
		return ErrUnknown
	}
	file, err := os.CreateTemp(j.Directory, ".ledger-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer func() {
		if result != nil {
			_ = os.Remove(name)
		}
	}()
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, filepath.Join(j.Directory, "ledger.json")); err != nil {
		return err
	}
	return syncDirectory(j.Directory)
}
