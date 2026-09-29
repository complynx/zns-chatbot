package migrate

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

const maxUserPlanBytes = 256 << 20
const usersSource = "users"
const recordsFileKind = "records"

// UserPlanSummary is safe for stdout. The separately selected output is private.
type UserPlanSummary struct {
	ManifestSHA256 string `json:"manifest_sha256"`
	ArtifactSHA256 string `json:"artifact_sha256"`
	Candidates     int64  `json:"candidates"`
	Excluded       int64  `json:"excluded_other_bot"`
	Invalid        int64  `json:"invalid_records"`
	Blocked        int64  `json:"blocked_records"`
	ApplyReady     bool   `json:"apply_ready"`
	Reused         bool   `json:"reused"`
}

// PlanUsers reads only a verified immutable stage. It never applies a plan.
func PlanUsers(stage, target string, limits Limits) (UserPlanSummary, error) {
	if err := limits.validate(); err != nil {
		return UserPlanSummary{}, err
	}
	root, report, err := verifiedUsersStage(stage, limits)
	if err != nil {
		return UserPlanSummary{}, err
	}
	defer func() { _ = root.Close() }()
	manifest, err := readVerifiedUsersManifest(root, report, limits)
	if err != nil {
		return UserPlanSummary{}, err
	}
	if manifest.BotID >= maxTelegramID {
		return UserPlanSummary{}, errors.New("bot_id_outside_target_range")
	}
	included := false
	for _, source := range manifest.Coverage {
		if source.Domain == usersSource && source.Status == sourceIncluded {
			included = true
		}
	}
	if !included {
		return UserPlanSummary{}, errors.New("users_source_required")
	}
	return writeUsersPlan(stage, target, root, manifest, report, limits)
}

func readVerifiedUsersManifest(root *os.Root, report Report, limits Limits) (Manifest, error) {
	manifest, data, err := readManifest(root, limits)
	if err != nil {
		return Manifest{}, err
	}
	if hashBytes(data) != report.ManifestSHA256 {
		return Manifest{}, errors.New("users_manifest_changed")
	}
	return manifest, nil
}

func verifiedUsersStage(stage string, limits Limits) (*os.Root, Report, error) {
	absolute, err := filepath.Abs(stage)
	if err != nil {
		return nil, Report{}, errors.New("invalid_stage_path")
	}
	parent, err := openDirectory(filepath.Dir(absolute))
	if err != nil {
		return nil, Report{}, err
	}
	defer func() { _ = parent.Close() }()
	stageRoot, err := openDirectory(absolute)
	if err != nil {
		return nil, Report{}, err
	}
	defer func() { _ = stageRoot.Close() }()
	root, err := stageRoot.OpenRoot("snapshot")
	if err != nil {
		return nil, Report{}, errors.New("stage_snapshot_missing")
	}
	report, err := verifyRoot(root, limits)
	if err == nil {
		err = verifyExistingStage(parent, filepath.Base(absolute), report, limits)
	}
	if err != nil {
		_ = root.Close()
		return nil, Report{}, err
	}
	return root, report, nil
}

func emitUsersPlan(
	writer io.Writer,
	root *os.Root,
	manifest Manifest,
	report Report,
	limits Limits,
) (UserPlanSummary, error) {
	summary := UserPlanSummary{ManifestSHA256: report.ManifestSHA256}
	encoder := json.NewEncoder(writer)
	header := struct {
		Kind           string `json:"kind"`
		Version        int    `json:"version"`
		ManifestSHA256 string `json:"manifest_sha256"`
		BotID          int64  `json:"bot_id"`
	}{Kind: "users-plan", Version: 1, ManifestSHA256: report.ManifestSHA256, BotID: manifest.BotID}
	if encoder.Encode(header) != nil {
		return summary, errors.New("plan_write_failed")
	}
	collection := ""
	for _, source := range manifest.Coverage {
		if source.Domain == usersSource {
			collection = source.Name
		}
	}
	registry, err := userPassRegistry(root, manifest, limits)
	if err != nil {
		return summary, err
	}
	seen := map[int64]bool{}
	for _, file := range manifest.Files {
		if file.Source != usersSource {
			continue
		}
		if file.Kind != recordsFileKind {
			return summary, errors.New("users_source_requires_records")
		}
		if err = emitUserFile(
			encoder,
			root,
			file,
			manifest.BotID,
			collection,
			limits,
			seen,
			registry,
			&summary,
		); err != nil {
			return summary, err
		}
	}
	if encoder.Encode(struct {
		Kind    string          `json:"kind"`
		Summary UserPlanSummary `json:"summary"`
	}{Kind: "summary", Summary: summary}) != nil {
		return summary, errors.New("plan_write_failed")
	}
	return summary, nil
}

func emitUserFile(
	encoder *json.Encoder,
	root *os.Root,
	entry File,
	botID int64,
	collection string,
	limits Limits,
	seen map[int64]bool,
	registry map[string]bool,
	summary *UserPlanSummary,
) error {
	file, err := openRegular(root, entry.Path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	digest := sha256.New()
	bounded := &io.LimitedReader{R: file, N: entry.Bytes + 1}
	scanner := bufio.NewScanner(io.TeeReader(bounded, digest))
	scanner.Buffer(make([]byte, min(recordReadBuffer, limits.RecordBytes+1)), limits.RecordBytes+1)
	var line int64
	for scanner.Scan() {
		line++
		if line > entry.Records {
			return errors.New("users_source_changed")
		}
		data := scanner.Bytes()
		if len(data) > limits.RecordBytes || validJSON(data) != nil {
			return errors.New("users_record_invalid")
		}
		var record map[string]json.RawMessage
		if json.Unmarshal(data, &record) != nil || record == nil {
			return errors.New("users_record_invalid")
		}
		id, parseErr := recordID(record["_id"])
		if parseErr != nil {
			return errors.New("users_record_id_invalid")
		}
		planned := convertUser(record, botID)
		deferUserPassFields(record, &planned, registry)
		deferUserFoodFields(record, &planned, registry)
		deferUserMassageFields(record, &planned)
		planned.Legacy = UserLegacyReference{
			Key:          identityKey("users:"+strconv.FormatInt(botID, 10)+":"+collection, id),
			Collection:   collection,
			RecordID:     record["_id"],
			File:         entry.Path,
			Line:         line,
			RecordSHA256: hashBytes(data),
		}
		if err = countPlannedUser(planned, seen, summary); err != nil {
			return err
		}
		if encoder.Encode(planned) != nil {
			return errors.New("plan_write_failed")
		}
	}
	if scanner.Err() != nil || line != entry.Records || bounded.N != 1 ||
		hex.EncodeToString(digest.Sum(nil)) != entry.SHA256 {
		return errors.New("users_source_changed")
	}
	return nil
}

func countPlannedUser(planned UserPlanRecord, seen map[int64]bool, summary *UserPlanSummary) error {
	switch planned.Status {
	case userExcluded:
		summary.Excluded++
	case "blocked":
		summary.Invalid++
	case userCandidate:
		id := planned.Candidate.TelegramID
		if seen[id] {
			return errors.New("duplicate_telegram_identity")
		}
		seen[id] = true
		summary.Candidates++
	}
	if len(planned.Blockers) > 0 {
		summary.Blocked++
	}
	return nil
}
