package legacyimport

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
)

const (
	bundleFramingAllowance  = 1_048_576
	payloadFramingAllowance = 16_384
	MaxBundleBytes          = maxSourceBytes + bundleFramingAllowance
	maxBundlePayloadBytes   = maxSourceBytes + 3*maxManifestBytes + payloadFramingAllowance
)

var ErrInvalidBundle = errors.New("invalid legacy snapshot archive")

type BundleValidation struct {
	Archive     ArchiveReceipt `json:"archive"`
	Inspection  Report         `json:"inspection"`
	SourceBotID int64          `json:"-"`
}

// ValidateBundle verifies the receiving stream without extraction, storage or runtime import.
func ValidateBundle(ctx context.Context, input io.Reader, expectedSHA256 string) (BundleValidation, error) {
	return VisitBundle(ctx, input, expectedSHA256, nil)
}

// VisitBundle visits individual validated records before final archive verification.
// Visitor effects must remain provisional until this function succeeds: later
// source checksums, reports, tar framing or gzip checks can still reject the archive.
// Visitor errors are returned unchanged and stop further callbacks.
func VisitBundle(
	ctx context.Context,
	input io.Reader,
	expectedSHA256 string,
	visitor func(Record) error,
) (BundleValidation, error) {
	return visitBundle(ctx, input, expectedSHA256, visitor, nil)
}

func visitBundle(ctx context.Context, input io.Reader, expectedSHA256 string,
	visitor func(Record) error, sourceVisitor func(Record, map[string]any) error,
) (BundleValidation, error) {
	expected, err := hex.DecodeString(expectedSHA256)
	if err != nil || len(expected) != sha256.Size || hex.EncodeToString(expected) != expectedSHA256 {
		return BundleValidation{}, ErrInvalidBundle
	}
	hash := sha256.New()
	counted := &countedReader{reader: io.LimitReader(contextReader{ctx: ctx, reader: input}, MaxBundleBytes+1)}
	compressed := bufio.NewReader(io.TeeReader(counted, hash))
	decoded, err := gzip.NewReader(compressed)
	if err != nil {
		return BundleValidation{}, ErrInvalidBundle
	}
	defer func() { _ = decoded.Close() }()
	decoded.Multistream(false)
	payload := &countedReader{reader: io.LimitReader(contextReader{ctx: ctx, reader: decoded}, maxBundlePayloadBytes+1)}
	format := bundleTarReader{reader: payload}
	m, err := format.manifests()
	if err != nil {
		return BundleValidation{}, ErrInvalidBundle
	}
	report, err := format.sources(ctx, m, visitor, sourceVisitor)
	if err != nil {
		return BundleValidation{}, err
	}
	if !report.ValidationPassed {
		return BundleValidation{}, ErrInvalidBundle
	}
	if err = format.report(report); err != nil {
		return BundleValidation{}, ErrInvalidBundle
	}
	if err = format.end(); err != nil {
		return BundleValidation{}, ErrInvalidBundle
	}
	if _, err = compressed.ReadByte(); !errors.Is(err, io.EOF) || counted.bytes > MaxBundleBytes ||
		payload.bytes > maxBundlePayloadBytes ||
		ctx.Err() != nil {
		return BundleValidation{}, ErrInvalidBundle
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expectedSHA256 {
		return BundleValidation{}, ErrInvalidBundle
	}
	return BundleValidation{SourceBotID: m.BotID, Inspection: report, Archive: ArchiveReceipt{
		Format: bundleFormat, SHA256: actual, SizeBytes: counted.bytes,
	}}, nil
}

func (r *bundleTarReader) manifests() (Manifest, error) {
	normalized, err := r.metadata(bundleManifestName)
	if err != nil {
		return Manifest{}, err
	}
	m, err := parseManifest(normalized)
	if err != nil {
		return Manifest{}, err
	}
	original, err := r.metadata(originalManifestName)
	if err != nil {
		return Manifest{}, err
	}
	originalManifest, err := parseManifest(original)
	if err != nil {
		return Manifest{}, err
	}
	expected, err := json.Marshal(normalizeManifest(originalManifest))
	if err != nil || !bytes.Equal(normalized, append(expected, '\n')) {
		return Manifest{}, ErrInvalidBundle
	}
	return m, nil
}

func (r *bundleTarReader) sources(ctx context.Context, m Manifest, visitor func(Record) error,
	sourceVisitor func(Record, map[string]any) error) (Report, error) {
	i := newInspector(m, nil)
	i.visitor = withVisitContext(ctx, visitor)
	i.sourceVisitor = sourceVisitor
	for _, role := range []string{roleUsers, roleMessages, rolePasses, roleFiles} {
		source := &SourceReport{Role: role}
		for _, c := range m.Collections {
			if c.Role != role {
				continue
			}
			body, size, err := r.entry(sourceArchivePath(role), maxSourceBytes-i.bytes)
			if err != nil {
				return Report{}, err
			}
			source = i.scanSource(c, body, io.Discard)
			if i.visitErr != nil {
				return Report{}, i.visitErr
			}
			if !i.report.ValidationPassed || body.N != 0 {
				return Report{}, ErrInvalidBundle
			}
			if err = r.padding(size); err != nil {
				return Report{}, err
			}
		}
		i.report.Sources = append(i.report.Sources, *source)
	}
	return i.report, nil
}

func withVisitContext(ctx context.Context, visitor func(Record) error) func(Record) error {
	if visitor == nil {
		return nil
	}
	return func(record Record) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return visitor(record)
	}
}

func (r *bundleTarReader) report(report Report) error {
	raw, err := r.metadata(inspectionReportName)
	if err != nil {
		return err
	}
	expected, err := json.Marshal(report)
	if err != nil || !bytes.Equal(raw, append(expected, '\n')) {
		return ErrInvalidBundle
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(raw []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(raw)
}
