package legacyimport

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	maxLineBytes       = 8 << 20
	maxSourceBytes     = 256 << 20
	maxRecords         = 100000
	maxIssues          = 1000
	scannerBufferBytes = 64 << 10
)

type Issue struct {
	Role string `json:"role"`
	Line int    `json:"line"`
	Code string `json:"code"`
}

type SourceReport struct {
	Role    string `json:"role"`
	Present bool   `json:"present"`
	Records int    `json:"records"`
	Bytes   int64  `json:"bytes"`
	SHA256  string `json:"sha256,omitempty"`
}

type Report struct {
	Version                 int            `json:"version"`
	SourceInstance          string         `json:"source_instance"`
	ValidationPassed        bool           `json:"validation_passed"`
	ImportPerformed         bool           `json:"import_performed"`
	CompleteTelegramHistory bool           `json:"complete_telegram_history"`
	Sources                 []SourceReport `json:"sources"`
	Issues                  []Issue        `json:"issues"`
	IssuesTruncated         bool           `json:"issues_truncated"`
}

type inspector struct {
	manifest      Manifest
	report        Report
	users         map[int64]bool
	bytes         int64
	records       int
	seenFiles     []os.FileInfo
	archive       *archiveSink
	visitor       func(Record) error
	sourceVisitor func(Record, map[string]any) error
	visitErr      error
}

// Inspect reads a bound local export. It never opens databases or changes source files.
func Inspect(manifestPath string) (Report, error) {
	m, err := readManifest(manifestPath)
	if err != nil {
		return Report{}, err
	}
	root, err := os.OpenRoot(filepath.Dir(manifestPath))
	if err != nil {
		return Report{}, ErrManifest
	}
	defer func() { _ = root.Close() }()
	return inspectSnapshot(root, m, nil), nil
}

func inspectSnapshot(root *os.Root, m Manifest, archive *archiveSink) Report {
	i := newInspector(m, archive)
	for _, role := range []string{roleUsers, roleMessages, rolePasses, roleFiles} {
		source := &SourceReport{Role: role}
		for _, c := range m.Collections {
			if c.Role == role {
				source = i.inspectSource(root, c)
				break
			}
		}
		i.report.Sources = append(i.report.Sources, *source)
		if !i.report.ValidationPassed {
			// A rejected source may leave its tar entry incomplete; only inspection continues.
			i.archive = nil
		}
	}
	return i.report
}

func newInspector(m Manifest, archive *archiveSink) inspector {
	return inspector{manifest: m, users: map[int64]bool{}, archive: archive, report: Report{
		Version:          1,
		SourceInstance:   m.SourceInstance,
		ValidationPassed: true,
		Sources:          []SourceReport{},
		Issues:           []Issue{},
	}}
}

func (i *inspector) issue(role string, line int, code string) {
	i.report.ValidationPassed = false
	if len(i.report.Issues) < maxIssues {
		i.report.Issues = append(i.report.Issues, Issue{Role: role, Line: line, Code: code})
	} else {
		i.report.IssuesTruncated = true
	}
}

func (i *inspector) inspectSource(root *os.Root, c Collection) *SourceReport {
	r := &SourceReport{Role: c.Role, Present: true}
	info, err := root.Lstat(c.Path)
	if err != nil || !info.Mode().IsRegular() {
		i.issue(c.Role, 0, "source_limit_or_type")
		return r
	}
	f, err := root.Open(c.Path)
	if err != nil {
		i.issue(c.Role, 0, "source_unavailable")
		return r
	}
	defer func() { _ = f.Close() }()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxSourceBytes-i.bytes {
		i.issue(c.Role, 0, "source_limit_or_type")
		return r
	}
	if i.seenSourceFile(info) {
		i.issue(c.Role, 0, "duplicate_source_file")
		return r
	}
	sink, err := sourceWriter(i.archive, c.Role, info.Size())
	if err != nil {
		i.issue(c.Role, 0, "archive_write_unavailable")
		return r
	}
	return i.scanSource(c, f, sink)
}

func (i *inspector) scanSource(c Collection, input io.Reader, output io.Writer) *SourceReport {
	r := &SourceReport{Role: c.Role, Present: true}
	hash := sha256.New()
	remaining := maxSourceBytes - i.bytes
	counted := &countedReader{reader: io.LimitReader(input, remaining+1)}
	defer func() { r.Bytes = counted.bytes; i.bytes += counted.bytes }()
	reader := io.TeeReader(counted, io.MultiWriter(hash, output))
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, scannerBufferBytes), maxLineBytes)
	seen := map[string][32]byte{}
	for scanner.Scan() {
		r.Records++
		i.records++
		if i.records > maxRecords {
			i.issue(c.Role, r.Records, "record_limit")
			return r
		}
		i.inspectRecord(c, r.Records, scanner.Bytes(), seen)
		if i.visitErr != nil {
			return r
		}
	}
	if scanner.Err() != nil {
		i.issue(c.Role, r.Records+1, "source_read_or_line_limit")
		return r
	}
	if counted.bytes > remaining {
		i.issue(c.Role, 0, "source_limit")
		return r
	}
	r.SHA256 = hex.EncodeToString(hash.Sum(nil))
	if r.SHA256 != c.SHA256 {
		i.issue(c.Role, 0, "checksum_mismatch")
	}
	return r
}

func (i *inspector) seenSourceFile(info os.FileInfo) bool {
	for _, previous := range i.seenFiles {
		if os.SameFile(previous, info) {
			return true
		}
	}
	i.seenFiles = append(i.seenFiles, info)
	return false
}

type countedReader struct {
	reader io.Reader
	bytes  int64
}

func (r *countedReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytes += int64(n)
	return n, err
}

func (i *inspector) inspectRecord(c Collection, line int, raw []byte, seen map[string][32]byte) {
	v, err := decodeJSON(raw)
	object, ok := v.(map[string]any)
	if err != nil || !ok {
		i.issue(c.Role, line, "invalid_record_json")
		return
	}
	key, ok := sourceKey(object["_id"])
	if !ok {
		i.issue(c.Role, line, "invalid_source_id")
		return
	}
	canonical, _ := json.Marshal(object)
	digest := sha256.Sum256(canonical)
	if previous, exists := seen[key]; exists {
		code := "duplicate_source_id"
		if previous != digest {
			code = "conflicting_source_id"
		}
		i.issue(c.Role, line, code)
		return
	}
	seen[key] = digest
	if bot, present := object["bot_id"]; present {
		id, valid := identityInteger(bot)
		if !valid || id != i.manifest.BotID {
			i.issue(c.Role, line, "bot_scope_mismatch")
			return
		}
	} else if c.Role != roleMessages {
		i.issue(c.Role, line, "missing_bot_id")
		return
	}
	user, valid := identityInteger(object["user_id"])
	if !valid || !validTelegramID(user) {
		i.issue(c.Role, line, "invalid_owner_id")
		return
	}
	if c.Role == roleUsers {
		if i.users[user] {
			i.issue(c.Role, line, "conflicting_owner_id")
		} else {
			i.users[user] = true
		}
	} else if !i.users[user] {
		i.issue(c.Role, line, "orphan_owner")
	}
	if c.Role == roleMessages && !sourceDate(object["date"]) {
		i.issue(c.Role, line, "unsupported_source_date")
	}
	i.emitRecord(Record{SourceInstance: i.manifest.SourceInstance, Collection: c.Name,
		Role: c.Role, SourceKey: key, OwnerID: user, Line: line, SHA256: digest}, object)
}

func (i *inspector) emitRecord(record Record, object map[string]any) {
	if !i.report.ValidationPassed {
		return
	}
	if i.visitor != nil {
		record.Projection = projectRecord(record.Role, object)
		i.visitErr = i.visitor(record)
	}
	if i.visitErr == nil && i.sourceVisitor != nil {
		i.visitErr = i.sourceVisitor(record, object)
	}
}

// identityInteger accepts integral BSON doubles without rounding decimal source
// values. Timestamp parsing keeps the stricter integer representation below.
func identityInteger(v any) (int64, bool) {
	if id, ok := integer(v); ok {
		return id, true
	}
	var text string
	switch value := v.(type) {
	case json.Number:
		text = value.String()
	case map[string]any:
		if len(value) != 1 {
			return 0, false
		}
		text, _ = value["$numberDouble"].(string)
	default:
		return 0, false
	}
	// BSON double spellings are short; bound arbitrary exponents before allocating
	// an exact rational, including JSON numbers that would underflow a float.
	if len(text) == 0 || len(text) > 64 || !json.Valid([]byte(text)) {
		return 0, false
	}
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		exponent, err := strconv.Atoi(text[index+1:])
		if err != nil || exponent < -400 || exponent > 400 {
			return 0, false
		}
	}
	value, ok := new(big.Rat).SetString(text)
	if !ok || !value.IsInt() || !value.Num().IsInt64() {
		return 0, false
	}
	return value.Num().Int64(), true
}

func integer(v any) (int64, bool) {
	var text string
	switch value := v.(type) {
	case json.Number:
		text = value.String()
	case map[string]any:
		if len(value) != 1 {
			return 0, false
		}
		text, _ = value["$numberLong"].(string)
		if text == "" {
			text, _ = value["$numberInt"].(string)
		}
	default:
		return 0, false
	}
	id, err := strconv.ParseInt(text, 10, 64)
	return id, err == nil
}

func sourceKey(v any) (string, bool) {
	switch value := v.(type) {
	case string:
		if len(value) == 0 || len(value) > 256 {
			return "", false
		}
	case map[string]any:
		oid, ok := value["$oid"].(string)
		decoded, err := hex.DecodeString(oid)
		if !ok || len(value) != 1 || err != nil || len(decoded) != 12 {
			return "", false
		}
		return "oid:" + hex.EncodeToString(decoded), true
	default:
		return "", false
	}
	raw, err := json.Marshal(v)
	return string(raw), err == nil
}

func sourceDate(v any) bool {
	object, ok := v.(map[string]any)
	if !ok || len(object) != 1 {
		return false
	}
	value, ok := object["$date"]
	if !ok {
		return false
	}
	if text, yes := value.(string); yes {
		_, err := time.Parse(time.RFC3339Nano, text)
		return err == nil
	}
	_, ok = integer(value)
	return ok
}
