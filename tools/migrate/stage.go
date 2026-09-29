package migrate

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const stageReceipt = "stage.json"
const privateFileMode = 0o600
const privateDirectoryMode = 0o700
const stageEntryCount = 2

// Stage copies verified bytes into a new bundle. Existing bundles are verified
// and reused only on an exact replay. It never overwrites an existing target.
func Stage(snapshot, target string, limits Limits) (Report, bool, error) {
	report, err := Verify(snapshot, limits)
	if err != nil {
		return report, false, err
	}
	absolute, err := filepath.Abs(target)
	if err != nil || overlaps(snapshot, absolute) {
		return report, false, errors.New("invalid_stage_target")
	}
	name := filepath.Base(absolute)
	if !safeRelative(name) {
		return report, false, errors.New("invalid_stage_name")
	}
	parent, err := openDirectory(filepath.Dir(absolute))
	if err != nil {
		return report, false, err
	}
	defer func() { _ = parent.Close() }()
	lockName := "." + name + ".lock"
	lock, err := parent.OpenFile(lockName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, privateFileMode)
	if err != nil {
		return report, false, errors.New("stage_locked")
	}
	defer func() { _ = lock.Close(); _ = parent.Remove(lockName) }()
	if _, err = parent.Lstat(name); err == nil {
		err = verifyExistingStage(parent, name, report, limits)
		return report, err == nil, err
	} else if !os.IsNotExist(err) {
		return report, false, errors.New("stage_target_unavailable")
	}
	return createStage(parent, name, snapshot, report, limits)
}

func overlaps(first, second string) bool {
	first, err := filepath.Abs(first)
	if err != nil {
		return true
	}
	first = strings.ToLower(filepath.Clean(first))
	second = strings.ToLower(filepath.Clean(second))
	separator := string(filepath.Separator)
	return first == second || strings.HasPrefix(first, second+separator) || strings.HasPrefix(second, first+separator)
}

func createStage(parent *os.Root, name, snapshot string, report Report, limits Limits) (Report, bool, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return report, false, errors.New("stage_random_failed")
	}
	temporary := "." + name + ".tmp-" + hex.EncodeToString(nonce[:])
	if err := parent.Mkdir(temporary, privateDirectoryMode); err != nil {
		return report, false, errors.New("stage_create_failed")
	}
	defer func() { _ = parent.RemoveAll(temporary) }()
	root, err := parent.OpenRoot(temporary)
	if err != nil {
		return report, false, errors.New("stage_open_failed")
	}
	err = fillStage(root, snapshot, report, limits)
	closeErr := root.Close()
	if err != nil {
		return report, false, err
	}
	if closeErr != nil {
		return report, false, errors.New("stage_close_failed")
	}
	if err = parent.Rename(temporary, name); err != nil {
		return report, false, errors.New("stage_publish_failed")
	}
	return report, false, nil
}

func fillStage(target *os.Root, snapshot string, report Report, limits Limits) error {
	source, err := openDirectory(snapshot)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	manifest, data, err := readManifest(source, limits)
	if err != nil {
		return err
	}
	if hashBytes(data) != report.ManifestSHA256 {
		return errors.New("source_changed")
	}
	if err = target.Mkdir("snapshot", privateDirectoryMode); err != nil {
		return errors.New("stage_create_failed")
	}
	copyRoot, err := target.OpenRoot("snapshot")
	if err != nil {
		return errors.New("stage_open_failed")
	}
	defer func() { _ = copyRoot.Close() }()
	if err = writeStageFile(copyRoot, manifestName, data); err != nil {
		return err
	}
	for _, entry := range manifest.Files {
		if err = copyStageFile(source, copyRoot, entry); err != nil {
			return err
		}
	}
	copied, err := verifyRoot(copyRoot, limits)
	if err != nil {
		return err
	}
	if !sameReport(copied, report) {
		return errors.New("source_changed")
	}
	receipt, _ := json.Marshal(report)
	return writeStageReceipt(target, receipt)
}

func writeStageReceipt(target *os.Root, data []byte) error {
	return writeStageFile(target, stageReceipt, data)
}

func writeStageFile(target *os.Root, name string, data []byte) error {
	file, err := target.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateFileMode)
	if err != nil {
		return errors.New("stage_receipt_write_failed")
	}
	defer func() { _ = file.Close() }()
	if _, err = file.Write(data); err != nil {
		return errors.New("stage_receipt_write_failed")
	}
	if err = file.Sync(); err != nil {
		return errors.New("stage_sync_failed")
	}
	return nil
}

func copyStageFile(source, target *os.Root, entry File) error {
	input, err := openRegular(source, entry.Path)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	directory := filepath.ToSlash(filepath.Dir(entry.Path))
	if directory != "." {
		if err = target.MkdirAll(directory, privateDirectoryMode); err != nil {
			return errors.New("stage_directory_failed")
		}
	}
	output, err := target.OpenFile(entry.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateFileMode)
	if err != nil {
		return errors.New("stage_file_create_failed")
	}
	count, copyErr := io.Copy(output, io.LimitReader(input, entry.Bytes+1))
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil || count != entry.Bytes {
		return errors.New("stage_copy_failed")
	}
	return nil
}

func verifyExistingStage(parent *os.Root, name string, report Report, limits Limits) error {
	info, err := parent.Lstat(name)
	if err != nil || !info.IsDir() || forbiddenLink(info) {
		return errors.New("unsafe_stage_target")
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return errors.New("stage_open_failed")
	}
	defer func() { _ = root.Close() }()
	directory, err := root.Open(".")
	if err != nil {
		return errors.New("stage_open_failed")
	}
	entries, readErr := directory.ReadDir(stageEntryCount + 1)
	_ = directory.Close()
	if readErr != nil && readErr != io.EOF || len(entries) != stageEntryCount {
		return errors.New("stage_contents_changed")
	}
	snapshotInfo, err := root.Lstat("snapshot")
	if err != nil || !snapshotInfo.IsDir() || forbiddenLink(snapshotInfo) {
		return errors.New("unsafe_staged_snapshot")
	}
	snapshot, err := root.OpenRoot("snapshot")
	if err != nil {
		return errors.New("stage_open_failed")
	}
	defer func() { _ = snapshot.Close() }()
	actual, err := verifyRoot(snapshot, limits)
	if err != nil || !sameReport(report, actual) {
		return errors.New("stage_snapshot_mismatch")
	}
	file, err := openRegular(root, stageReceipt)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
	expected, _ := json.Marshal(report)
	if err != nil || !bytes.Equal(data, expected) {
		return errors.New("stage_receipt_mismatch")
	}
	return nil
}

func sameReport(a, b Report) bool {
	first, _ := json.Marshal(a)
	second, _ := json.Marshal(b)
	return bytes.Equal(first, second)
}
