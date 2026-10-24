package legacyimport

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
)

const (
	tarBlockBytes = 512
	tarTypeOffset = 156
)

// Read only our fixed regular USTAR entries; reject extensions before [tar.Reader] can consume them.
type bundleTarReader struct{ reader io.Reader }

func (r *bundleTarReader) entry(name string, maxBytes int64) (*io.LimitedReader, int64, error) {
	var raw [tarBlockBytes]byte
	if _, err := io.ReadFull(r.reader, raw[:]); err != nil {
		return nil, 0, ErrInvalidBundle
	}
	if raw[tarTypeOffset] != tar.TypeReg {
		return nil, 0, ErrInvalidBundle
	}
	header, err := tar.NewReader(bytes.NewReader(raw[:])).Next()
	if err != nil || header.Name != name || header.Format != tar.FormatUSTAR ||
		header.Mode != privateArchiveMode || header.Uid != 0 || header.Gid != 0 ||
		header.Uname != "" || header.Gname != "" || header.Linkname != "" || header.ModTime.Unix() != 0 ||
		!header.AccessTime.IsZero() || !header.ChangeTime.IsZero() || len(header.PAXRecords) != 0 ||
		header.Size < 0 || header.Size > maxBytes {
		return nil, 0, ErrInvalidBundle
	}
	return &io.LimitedReader{R: r.reader, N: header.Size}, header.Size, nil
}

func (r *bundleTarReader) metadata(name string) ([]byte, error) {
	body, size, err := r.entry(name, maxManifestBytes)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(body)
	if err != nil || body.N != 0 {
		return nil, ErrInvalidBundle
	}
	if err = r.padding(size); err != nil {
		return nil, err
	}
	return raw, nil
}

func (r *bundleTarReader) padding(size int64) error {
	padding := (tarBlockBytes - size%tarBlockBytes) % tarBlockBytes
	var raw [tarBlockBytes]byte
	if _, err := io.ReadFull(r.reader, raw[:padding]); err != nil || !allZero(raw[:padding]) {
		return ErrInvalidBundle
	}
	return nil
}

func (r *bundleTarReader) end() error {
	var raw [2 * tarBlockBytes]byte
	if _, err := io.ReadFull(r.reader, raw[:]); err != nil || !allZero(raw[:]) {
		return ErrInvalidBundle
	}
	var extra [1]byte
	if n, err := r.reader.Read(extra[:]); n != 0 || !errors.Is(err, io.EOF) {
		return ErrInvalidBundle
	}
	return nil
}

func allZero(raw []byte) bool {
	for _, value := range raw {
		if value != 0 {
			return false
		}
	}
	return true
}
