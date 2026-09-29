package migrate

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// Report contains counts and fixed diagnostic codes, never source payloads or IDs.
// Verified proves file integrity, not business parity or completeness of production.
type Report struct {
	Verified       bool     `json:"verified"`
	ImportReady    bool     `json:"import_ready"`
	ManifestSHA256 string   `json:"manifest_sha256"`
	Files          int      `json:"files"`
	Records        int64    `json:"records"`
	Bytes          int64    `json:"bytes"`
	MissingProofs  int      `json:"missing_proofs"`
	AbsentSources  int      `json:"absent_sources"`
	Gaps           []string `json:"gaps"`
}

func Verify(directory string, limits Limits) (Report, error) {
	if err := limits.validate(); err != nil {
		return Report{}, err
	}
	root, err := openDirectory(directory)
	if err != nil {
		return Report{}, err
	}
	defer func() { _ = root.Close() }()
	return verifyRoot(root, limits)
}

func readManifest(root *os.Root, limits Limits) (Manifest, []byte, error) {
	file, err := openRegular(root, manifestName)
	if err != nil {
		return Manifest{}, nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
	if err != nil || len(data) > maxManifestBytes {
		return Manifest{}, nil, errors.New("manifest_read_limit")
	}
	manifest, err := decodeManifest(data)
	if err != nil {
		return Manifest{}, nil, err
	}
	if err = manifest.validate(limits); err != nil {
		return Manifest{}, nil, err
	}
	return manifest, data, nil
}

func verifyRoot(root *os.Root, limits Limits) (Report, error) {
	manifest, data, err := readManifest(root, limits)
	if err != nil {
		return Report{}, err
	}
	if err = inventory(root, manifest, limits); err != nil {
		return Report{}, err
	}
	report := Report{
		ManifestSHA256: hashBytes(data),
		Files:          len(manifest.Files),
		Gaps: []string{
			"domain_conversion_not_implemented",
			"identity_provisioning_not_verified",
			"coverage_is_operator_declared",
			"receipt_owner_linkage_not_verified",
			"receipt_reference_inventory_not_verified",
		},
	}
	identities := map[string]bool{}
	for index, file := range manifest.Files {
		if err = verifyFile(root, file, limits, identities); err != nil {
			return report, fmt.Errorf("file_%d: %w", index, err)
		}
		report.Bytes += file.Bytes
		report.Records += file.Records
	}
	for _, source := range manifest.Coverage {
		if source.Status == "absent" {
			report.AbsentSources++
		}
	}
	for index, proof := range manifest.Proofs {
		key, _ := recordID(proof.RecordID)
		if !identities[identityKey(proof.Source, key)] {
			return report, fmt.Errorf("proof_%d: source_record_missing", index)
		}
		owner, _ := recordID(proof.OwnerID)
		if !identities[identityKey("users", owner)] {
			return report, fmt.Errorf("proof_%d: owner_record_missing", index)
		}
		if proof.Unavailable {
			report.MissingProofs++
		}
	}
	if report.MissingProofs > 0 {
		report.Gaps = append(report.Gaps, "receipt_bytes_unavailable")
	}
	if report.AbsentSources > 0 {
		report.Gaps = append(report.Gaps, "declared_absences_require_review")
	}
	report.Verified = true
	return report, nil
}

func verifyFile(root *os.Root, entry File, limits Limits, identities map[string]bool) error {
	file, err := openRegular(root, entry.Path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || info.Size() != entry.Bytes {
		return errors.New("file_size_mismatch")
	}
	digest := sha256.New()
	bounded := &io.LimitedReader{R: file, N: entry.Bytes + 1}
	reader := io.TeeReader(bounded, digest)
	if entry.Kind == "records" {
		if err = verifyRecords(reader, entry, limits, identities); err != nil {
			return err
		}
	} else {
		if _, err = io.Copy(io.Discard, reader); err != nil {
			return errors.New("file_read_failed")
		}
	}
	if entry.Bytes+1-bounded.N != entry.Bytes {
		return errors.New("file_size_changed")
	}
	if hex.EncodeToString(digest.Sum(nil)) != entry.SHA256 {
		return errors.New("file_checksum_mismatch")
	}
	return nil
}

const recordReadBuffer = 64 * 1024

func verifyRecords(reader io.Reader, entry File, limits Limits, identities map[string]bool) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, min(recordReadBuffer, limits.RecordBytes+1)), limits.RecordBytes+1)
	var count int64
	for scanner.Scan() {
		count++
		if count > entry.Records || count > limits.Records {
			return errors.New("record_count_exceeded")
		}
		data := scanner.Bytes()
		if len(data) > limits.RecordBytes || validJSON(data) != nil {
			return fmt.Errorf("record_%d: invalid_record_json", count)
		}
		var record map[string]json.RawMessage
		if err := json.Unmarshal(data, &record); err != nil || record == nil {
			return fmt.Errorf("record_%d: record_not_object", count)
		}
		id, err := recordID(record["_id"])
		if err != nil {
			return fmt.Errorf("record_%d: invalid_record_id", count)
		}
		key := identityKey(entry.Source, id)
		if identities[key] {
			return fmt.Errorf("record_%d: duplicate_source_id", count)
		}
		identities[key] = true
	}
	if scanner.Err() != nil {
		return errors.New("record_read_limit")
	}
	if count != entry.Records {
		return errors.New("record_count_mismatch")
	}
	return nil
}

func hashBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func identityKey(source, id string) string { return hashBytes([]byte(source + "\x00" + id)) }
