package legacyimport

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	bundleManifestName   = "bundle-manifest.json"
	originalManifestName = "original-manifest.json"
	inspectionReportName = "inspection-report.json"
	bundleFormat         = "zinusya-legacy-snapshot/v1"
	privateArchiveMode   = 0o600
)

var ErrArchive = errors.New("snapshot archive unavailable")
var ErrArchiveCleanup = errors.New("snapshot archive temporary cleanup failed")

type ArchiveReceipt struct {
	Format    string `json:"format"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

type BundleResult struct {
	Inspection     Report          `json:"inspection"`
	Archive        *ArchiveReceipt `json:"archive,omitempty"`
	CleanupPending bool            `json:"cleanup_pending,omitempty"`
}

// CreateBundle validates and archives the same source bytes, then publishes without replacement.
// An invalid snapshot returns its report without an archive. No runtime data is imported.
func CreateBundle(manifestPath, destination string) (BundleResult, error) {
	m, raw, err := readManifestBytes(manifestPath)
	if err != nil {
		return BundleResult{}, err
	}
	root, err := os.OpenRoot(filepath.Dir(manifestPath))
	if err != nil {
		return BundleResult{}, ErrManifest
	}
	defer func() { _ = root.Close() }()
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".zinusya-legacy-*.tmp")
	if err != nil {
		return BundleResult{}, ErrArchive
	}
	cleanupAttempted := false
	defer func() {
		_ = temporary.Close()
		if !cleanupAttempted {
			_ = os.Remove(temporary.Name())
		}
	}()
	result, err := prepareBundle(root, m, raw, temporary)
	receipt := result.Archive
	result.Archive = nil
	if closeErr := temporary.Close(); closeErr != nil && err == nil {
		err = ErrArchive
	}
	if err == nil && receipt != nil {
		// Hard-link publication is atomic and fails if any destination entry exists.
		if linkErr := os.Link(temporary.Name(), destination); linkErr != nil {
			err = ErrArchive
		} else {
			result.Archive = receipt
		}
	}
	cleanupAttempted = true
	if removeErr := os.Remove(temporary.Name()); removeErr != nil {
		result.CleanupPending = true
		return result, ErrArchiveCleanup
	}
	return result, err
}

func prepareBundle(root *os.Root, m Manifest, raw []byte, temporary *os.File) (BundleResult, error) {
	hash := sha256.New()
	compressed := gzip.NewWriter(io.MultiWriter(temporary, hash))
	defer func() { _ = compressed.Close() }()
	sink := archiveSink{writer: tar.NewWriter(compressed)}
	defer func() { _ = sink.writer.Close() }()
	if err := writeBundleManifests(&sink, m, raw); err != nil {
		return BundleResult{}, ErrArchive
	}
	result := BundleResult{Inspection: inspectSnapshot(root, m, &sink)}
	if sink.err != nil {
		return result, ErrArchive
	}
	if !result.Inspection.ValidationPassed {
		return result, nil
	}
	if err := writeArchiveJSON(&sink, inspectionReportName, result.Inspection); err != nil {
		return result, ErrArchive
	}
	if err := closeBundle(&sink, compressed, temporary); err != nil {
		return result, ErrArchive
	}
	info, err := temporary.Stat()
	if err != nil {
		return result, ErrArchive
	}
	result.Archive = &ArchiveReceipt{
		Format:    bundleFormat,
		SHA256:    hex.EncodeToString(hash.Sum(nil)),
		SizeBytes: info.Size(),
	}
	return result, nil
}

func closeBundle(sink *archiveSink, compressed *gzip.Writer, temporary *os.File) error {
	if err := sink.writer.Close(); err != nil {
		return err
	}
	if err := compressed.Close(); err != nil {
		return err
	}
	return temporary.Sync()
}

func writeBundleManifests(sink *archiveSink, m Manifest, raw []byte) error {
	if err := writeArchiveJSON(sink, bundleManifestName, normalizeManifest(m)); err != nil {
		return err
	}
	if err := sink.start(originalManifestName, int64(len(raw))); err != nil {
		return err
	}
	_, err := sink.Write(raw)
	return err
}

func normalizeManifest(m Manifest) Manifest {
	normalized := m
	normalized.Collections = make([]Collection, 0, len(m.Collections))
	for _, role := range []string{roleUsers, roleMessages, rolePasses, roleFiles} {
		for _, collection := range m.Collections {
			if collection.Role == role {
				collection.Path = sourceArchivePath(role)
				normalized.Collections = append(normalized.Collections, collection)
			}
		}
	}
	return normalized
}

func writeArchiveJSON(sink *archiveSink, name string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err = sink.start(name, int64(len(raw))); err != nil {
		return err
	}
	_, err = sink.Write(raw)
	return err
}

func sourceArchivePath(role string) string { return "source/" + role + ".ndjson" }

func sourceWriter(sink *archiveSink, role string, size int64) (io.Writer, error) {
	if sink == nil {
		return io.Discard, nil
	}
	if err := sink.start(sourceArchivePath(role), size); err != nil {
		return nil, err
	}
	return sink, nil
}

type archiveSink struct {
	writer *tar.Writer
	err    error
}

func (s *archiveSink) start(name string, size int64) error {
	if s.err != nil {
		return s.err
	}
	s.err = s.writer.WriteHeader(&tar.Header{Name: name, Mode: privateArchiveMode, Size: size,
		Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0).UTC(), Format: tar.FormatUSTAR})
	return s.err
}

func (s *archiveSink) Write(raw []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	n, err := s.writer.Write(raw)
	s.err = err
	return n, err
}
