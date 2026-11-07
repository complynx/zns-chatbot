// Package frame provides the trusted frame asset without image codecs.
package frame

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"io"
)

//go:embed zns_frame.png
var png []byte

// Reader returns an independent read cursor over the immutable project asset.
func Reader() io.ReadSeeker { return bytes.NewReader(png) }
func SHA256() string        { hash := sha256.Sum256(png); return hex.EncodeToString(hash[:]) }
