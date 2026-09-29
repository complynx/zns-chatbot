package migrate

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
)

type planBudgetWriter struct {
	target    io.Writer
	remaining int64
}

func (w *planBudgetWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > w.remaining {
		return 0, errors.New("plan_output_limit")
	}
	count, err := w.target.Write(data)
	w.remaining -= int64(count)
	return count, err
}

func writeUsersPlan(
	stage, target string, source *os.Root, manifest Manifest, report Report, limits Limits,
) (UserPlanSummary, error) {
	var summary UserPlanSummary
	artifact, err := writePrivatePlan(stage, target, func(writer io.Writer) error {
		var emitErr error
		summary, emitErr = emitUsersPlan(writer, source, manifest, report, limits)
		return emitErr
	})
	summary.ArtifactSHA256, summary.Reused = artifact.ArtifactSHA256, artifact.Reused
	return summary, err
}

type planArtifact struct {
	ArtifactSHA256 string
	Reused         bool
}

func writePrivatePlan(stage, target string, emit func(io.Writer) error) (planArtifact, error) {
	absolute, err := filepath.Abs(target)
	if err != nil || overlaps(stage, absolute) {
		return planArtifact{}, errors.New("invalid_plan_target")
	}
	name := filepath.Base(absolute)
	if !safeRelative(name) {
		return planArtifact{}, errors.New("invalid_plan_name")
	}
	parent, err := openDirectory(filepath.Dir(absolute))
	if err != nil {
		return planArtifact{}, err
	}
	defer func() { _ = parent.Close() }()
	lockName := "." + name + ".lock"
	lock, err := parent.OpenFile(lockName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateFileMode)
	if err != nil {
		return planArtifact{}, errors.New("plan_locked")
	}
	defer func() { _ = lock.Close(); _ = parent.Remove(lockName) }()
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return planArtifact{}, errors.New("plan_random_failed")
	}
	temporary := "." + name + ".tmp-" + hex.EncodeToString(nonce[:])
	file, err := parent.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateFileMode)
	if err != nil {
		return planArtifact{}, errors.New("plan_create_failed")
	}
	defer func() { _ = file.Close(); _ = parent.Remove(temporary) }()
	digest := sha256.New()
	bounded := &planBudgetWriter{target: io.MultiWriter(file, digest), remaining: maxUserPlanBytes}
	summary := planArtifact{}
	err = emit(bounded)
	if err != nil {
		return summary, err
	}
	if err = file.Sync(); err != nil {
		return summary, errors.New("plan_sync_failed")
	}
	if err = file.Close(); err != nil {
		return summary, errors.New("plan_close_failed")
	}
	summary.ArtifactSHA256 = hex.EncodeToString(digest.Sum(nil))
	if _, err = parent.Lstat(name); err == nil {
		if err = verifyUserPlanReplay(parent, name, summary.ArtifactSHA256); err != nil {
			return summary, err
		}
		summary.Reused = true
		return summary, nil
	}
	if !os.IsNotExist(err) {
		return summary, errors.New("plan_target_unavailable")
	}
	if err = parent.Rename(temporary, name); err != nil {
		return summary, errors.New("plan_publish_failed")
	}
	return summary, nil
}

func verifyUserPlanReplay(root *os.Root, name, digest string) error {
	file, err := openRegular(root, name)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	count, err := io.Copy(hash, io.LimitReader(file, maxUserPlanBytes+1))
	if err != nil || count > maxUserPlanBytes || hex.EncodeToString(hash.Sum(nil)) != digest {
		return errors.New("plan_replay_mismatch")
	}
	return nil
}
