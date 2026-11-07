// Package legacyimport inspects offline source snapshots without importing runtime state.
package legacyimport

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
)

const (
	maxManifestBytes = 64 << 10
	roleUsers        = "users"
	roleMessages     = "messages"
	rolePasses       = "passes"
	roleFiles        = "files"
)

var ErrManifest = errors.New("invalid snapshot manifest")

type Manifest struct {
	Version        int          `json:"version"`
	SourceInstance string       `json:"source_instance"`
	BotID          int64        `json:"bot_id"`
	DatePolicy     string       `json:"date_policy"`
	Collections    []Collection `json:"collections"`
}

type Collection struct {
	Role                string `json:"role"`
	Name                string `json:"name"`
	Path                string `json:"path"`
	SHA256              string `json:"sha256"`
	SingleBotCollection bool   `json:"single_bot_collection,omitempty"`
}

func readManifest(path string) (Manifest, error) {
	m, _, err := readManifestBytes(path)
	return m, err
}

func readManifestBytes(path string) (Manifest, []byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return Manifest{}, nil, ErrManifest
	}
	f, err := os.Open(path) // #nosec G304 -- explicit operator-selected local snapshot manifest, read only.
	if err != nil {
		return Manifest{}, nil, ErrManifest
	}
	defer func() { _ = f.Close() }()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Manifest{}, nil, ErrManifest
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxManifestBytes+1))
	if err != nil || len(raw) > maxManifestBytes {
		return Manifest{}, nil, ErrManifest
	}
	m, err := parseManifest(raw)
	return m, raw, err
}

func parseManifest(raw []byte) (Manifest, error) {
	if len(raw) > maxManifestBytes {
		return Manifest{}, ErrManifest
	}
	value, err := decodeJSON(raw)
	if err != nil || !exactManifestFields(value) {
		return Manifest{}, ErrManifest
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var m Manifest
	if err = d.Decode(&m); err != nil || !validManifest(m) {
		return Manifest{}, ErrManifest
	}
	return m, nil
}

func exactManifestFields(value any) bool {
	object, ok := value.(map[string]any)
	if !ok || !allowedFields(object, "version", "source_instance", "bot_id", "date_policy", "collections") {
		return false
	}
	collections, ok := object["collections"].([]any)
	if !ok {
		return false
	}
	for _, value := range collections {
		collection, valid := value.(map[string]any)
		if !valid || !allowedFields(collection, "role", "name", "path", "sha256", "single_bot_collection") {
			return false
		}
	}
	return true
}

func allowedFields(object map[string]any, allowed ...string) bool {
	for field := range object {
		if !slices.Contains(allowed, field) {
			return false
		}
	}
	return true
}

func validManifest(m Manifest) bool {
	identifier := regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
	if m.Version != 1 || !identifier.MatchString(m.SourceInstance) || !validTelegramID(m.BotID) ||
		m.DatePolicy != "preserve_source" || len(m.Collections) == 0 || len(m.Collections) > 4 {
		return false
	}
	roles, paths, names := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, c := range m.Collections {
		hash, err := hex.DecodeString(c.SHA256)
		clean := filepath.Clean(c.Path)
		if !knownRole(c.Role) || roles[c.Role] || paths[clean] || names[c.Name] ||
			!identifier.MatchString(c.Name) || !filepath.IsLocal(c.Path) ||
			err != nil || len(hash) != 32 || hex.EncodeToString(hash) != c.SHA256 ||
			(c.Role == roleMessages && !c.SingleBotCollection) {
			return false
		}
		roles[c.Role], paths[clean], names[c.Name] = true, true, true
	}
	return roles[roleUsers]
}

func knownRole(role string) bool {
	return role == roleUsers || role == roleMessages || role == rolePasses || role == roleFiles
}

func validTelegramID(id int64) bool { return id > 0 && id < 1<<52 }
