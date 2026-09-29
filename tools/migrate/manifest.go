// Package migrate verifies offline snapshots, creates immutable staging bundles,
// and applies explicitly resolved user and event plans to PostgreSQL. It never connects to
// MongoDB, Telegram, or an identity provider.
package migrate

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
)

const manifestName = "manifest.json"
const maxManifestBytes = 1 << 20
const maxDepth = 64

const domainNames = "users events passes orders order_capacity massage messages files bot_storage knowledge schedule configuration"
const sourceIncluded = "included"
const defaultFileLimit = 256
const defaultRecordLimit = 1000000
const defaultTotalBytes = 2 << 30
const defaultFileBytes = 512 << 20
const defaultBlobBytes = 20 << 20
const defaultRecordBytes = 1 << 20

var tokenPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Limits bound every read and retained record index. All values must be positive.
type Limits struct {
	Files       int
	Records     int64
	TotalBytes  int64
	FileBytes   int64
	BlobBytes   int64
	RecordBytes int
}

func DefaultLimits() Limits {
	return Limits{
		Files:       defaultFileLimit,
		Records:     defaultRecordLimit,
		TotalBytes:  defaultTotalBytes,
		FileBytes:   defaultFileBytes,
		BlobBytes:   defaultBlobBytes,
		RecordBytes: defaultRecordBytes,
	}
}

func (v Limits) validate() error {
	if v.Files < 1 || v.Files > 10000 || v.Records < 1 || v.Records > 10000000 || v.TotalBytes < 1 ||
		v.TotalBytes > 1<<40 ||
		v.FileBytes < 1 ||
		v.FileBytes > v.TotalBytes ||
		v.BlobBytes < 1 ||
		v.BlobBytes > v.FileBytes ||
		v.RecordBytes < 1 ||
		v.RecordBytes > 16<<20 {
		return errors.New("invalid_limits")
	}
	return nil
}

type Manifest struct {
	Version     int      `json:"version"`
	SnapshotID  string   `json:"snapshot_id"`
	BotID       int64    `json:"bot_id"`
	CapturedAt  string   `json:"captured_at"`
	Consistency string   `json:"consistency"`
	Coverage    []Source `json:"coverage"`
	Files       []File   `json:"files"`
	Proofs      []Proof  `json:"proofs"`
}

type Source struct {
	Domain string `json:"domain"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type File struct {
	Path    string `json:"path"`
	Source  string `json:"source"`
	Kind    string `json:"kind"`
	SHA256  string `json:"sha256"`
	Bytes   int64  `json:"bytes"`
	Records int64  `json:"records"`
}

// Proof IDs and Telegram references stay in the private manifest, never diagnostic reports.
type Proof struct {
	Source         string          `json:"source"`
	RecordID       json.RawMessage `json:"record_id"`
	OwnerID        json.RawMessage `json:"owner_id"`
	Field          string          `json:"field"`
	TelegramFileID string          `json:"telegram_file_id"`
	ChatID         int64           `json:"chat_id"`
	MessageID      int64           `json:"message_id"`
	Blob           string          `json:"blob"`
	Unavailable    bool            `json:"unavailable"`
}

func (m Manifest) validate(limits Limits) error {
	if m.Version != 1 || !tokenPattern.MatchString(m.SnapshotID) || m.BotID <= 0 {
		return errors.New("invalid_manifest_identity")
	}
	if _, err := time.Parse(time.RFC3339Nano, m.CapturedAt); err != nil {
		return errors.New("invalid_capture_time")
	}
	if m.Consistency != "stopped_writer" && m.Consistency != "consistent_snapshot" {
		return errors.New("source_consistency_undeclared")
	}
	if len(m.Files) > limits.Files || len(m.Proofs) > limits.Files {
		return errors.New("manifest_capacity_exceeded")
	}
	sources, err := m.coverage()
	if err != nil {
		return err
	}
	if err = m.validateFiles(sources, limits); err != nil {
		return err
	}
	return m.validateProofs(sources)
}

func (m Manifest) validateFiles(sources map[string]string, limits Limits) error {
	seen := map[string]bool{}
	included := map[string]bool{}
	var total, records int64
	for _, file := range m.Files {
		if !safeRelative(file.Path) || file.Path == manifestName || seen[strings.ToLower(file.Path)] {
			return errors.New("invalid_or_duplicate_file_path")
		}
		seen[strings.ToLower(file.Path)] = true
		if sources[file.Source] != sourceIncluded {
			return errors.New("file_outside_declared_coverage")
		}
		included[file.Source] = true
		if err := file.validate(limits); err != nil {
			return err
		}
		total += file.Bytes
		records += file.Records
		if total > limits.TotalBytes || records > limits.Records {
			return errors.New("snapshot_capacity_exceeded")
		}
	}
	for domain, status := range sources {
		if status == sourceIncluded && !included[domain] {
			return errors.New("included_source_has_no_file")
		}
	}
	return nil
}

func (f File) validate(limits Limits) error {
	if !digestPattern.MatchString(f.SHA256) || f.Bytes < 0 || f.Bytes > limits.FileBytes || f.Records < 0 {
		return errors.New("invalid_file_metadata")
	}
	switch f.Kind {
	case "records":
		if f.Records > limits.Records {
			return errors.New("record_limit_exceeded")
		}
	case "blob":
		if f.Bytes == 0 || f.Bytes > limits.BlobBytes || f.Records != 0 {
			return errors.New("invalid_blob_metadata")
		}
	case "resource":
		if f.Records != 0 {
			return errors.New("invalid_resource_metadata")
		}
	default:
		return errors.New("unknown_file_kind")
	}
	return nil
}

func (m Manifest) coverage() (map[string]string, error) {
	sources := map[string]string{}
	for _, source := range m.Coverage {
		if sources[source.Domain] != "" || !knownDomain(source.Domain) || !tokenPattern.MatchString(source.Name) ||
			len(source.Reason) > 512 {
			return nil, errors.New("invalid_source_coverage")
		}
		if source.Status != sourceIncluded && source.Status != "absent" && source.Status != "unavailable" {
			return nil, errors.New("invalid_source_status")
		}
		if source.Status != sourceIncluded && strings.TrimSpace(source.Reason) == "" {
			return nil, errors.New("missing_source_reason")
		}
		if source.Status == "unavailable" {
			return nil, errors.New("source_unavailable")
		}
		sources[source.Domain] = source.Status
	}
	if len(sources) != len(strings.Fields(domainNames)) {
		return nil, errors.New("incomplete_source_coverage")
	}
	return sources, nil
}

func knownDomain(value string) bool {
	return slices.Contains(strings.Fields(domainNames), value)
}

func (m Manifest) validateProofs(sources map[string]string) error {
	blobs := map[string]bool{}
	for _, file := range m.Files {
		if file.Kind == "blob" {
			blobs[file.Path] = false
		}
	}
	for _, proof := range m.Proofs {
		if err := proof.validate(sources, blobs); err != nil {
			return err
		}
		if !proof.Unavailable {
			blobs[proof.Blob] = true
		}
	}
	for _, referenced := range blobs {
		if !referenced {
			return errors.New("unreferenced_proof_blob")
		}
	}
	return nil
}

func (p Proof) validate(sources map[string]string, blobs map[string]bool) error {
	if sources[p.Source] != sourceIncluded || p.Field == "" || len(p.Field) > 128 || p.TelegramFileID == "" ||
		len(p.TelegramFileID) > 1024 ||
		p.MessageID < 0 {
		return errors.New("invalid_proof_reference")
	}
	if _, err := recordID(p.RecordID); err != nil {
		return errors.New("invalid_proof_record_id")
	}
	if _, err := recordID(p.OwnerID); err != nil {
		return errors.New("invalid_proof_owner_id")
	}
	if p.Unavailable {
		if p.Blob != "" {
			return errors.New("unavailable_proof_has_blob")
		}
		return nil
	}
	if _, exists := blobs[p.Blob]; !exists {
		return errors.New("proof_blob_missing")
	}
	return nil
}
