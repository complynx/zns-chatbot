package migrate

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const maxPathDepth = 16
const inventoryReadBatch = 32

func safeRelative(name string) bool {
	if name == "" || len(name) > 1024 || strings.ContainsAny(name, `\:`) || strings.HasPrefix(name, "/") {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) > maxPathDepth {
		return false
	}
	for _, part := range parts {
		if part == "." || part == ".." || !tokenPattern.MatchString(part) || strings.HasSuffix(part, ".") {
			return false
		}
		stem := strings.ToUpper(strings.Split(part, ".")[0])
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" ||
			len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '0' &&
				stem[3] <= '9' {
			return false
		}
	}
	return true
}

// Reject links in selected root ancestry too; OpenRoot then enforces confinement
// even if an untrusted source changes a child after validation.
func openDirectory(name string) (*os.Root, error) {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return nil, errors.New("invalid_directory")
	}
	current := absolute
	for {
		info, statErr := os.Lstat(current)
		if statErr != nil || !info.IsDir() || forbiddenLink(info) {
			return nil, errors.New("unsafe_directory")
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, errors.New("directory_open_failed")
	}
	return root, nil
}

func openRegular(root *os.Root, name string) (*os.File, error) {
	if !safeRelative(name) {
		return nil, errors.New("unsafe_file_path")
	}
	components := strings.Split(name, "/")
	for index := range components {
		info, err := root.Lstat(strings.Join(components[:index+1], "/"))
		if err != nil || forbiddenLink(info) {
			return nil, errors.New("unsafe_or_missing_file")
		}
		if index < len(components)-1 && !info.IsDir() {
			return nil, errors.New("unsafe_file_parent")
		}
		if index == len(components)-1 && !info.Mode().IsRegular() {
			return nil, errors.New("not_regular_file")
		}
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, errors.New("file_open_failed")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, errors.New("not_regular_file")
	}
	return file, nil
}

func inventory(root *os.Root, manifest Manifest, limits Limits) error {
	expected := map[string]bool{manifestName: false}
	for _, file := range manifest.Files {
		expected[file.Path] = false
	}
	remaining := limits.Files*(maxPathDepth+1) + 1
	err := walkInventory(root, ".", &remaining, expected)
	if err != nil {
		return err
	}
	for _, present := range expected {
		if !present {
			return errors.New("declared_file_missing")
		}
	}
	return nil
}

func inventoryEntry(root *os.Root, name string, expected map[string]bool) (bool, error) {
	info, err := root.Lstat(name)
	if err != nil || forbiddenLink(info) {
		return false, errors.New("snapshot_link_forbidden")
	}
	if info.IsDir() {
		if name != "." && !safeRelative(name) {
			return false, errors.New("unsafe_directory_path")
		}
		return true, nil
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("not_regular_file")
	}
	if _, ok := expected[name]; !ok {
		return false, errors.New("undeclared_snapshot_file")
	}
	expected[name] = true
	return false, nil
}

func walkInventory(root *os.Root, name string, remaining *int, expected map[string]bool) error {
	directory, err := root.Open(name)
	if err != nil {
		return errors.New("snapshot_directory_open_failed")
	}
	defer func() { _ = directory.Close() }()
	return readInventoryEntries(directory, remaining, func(entry fs.DirEntry) error {
		child := path.Join(name, entry.Name())
		isDirectory, entryErr := inventoryEntry(root, child, expected)
		if entryErr != nil {
			return entryErr
		}
		if isDirectory {
			return walkInventory(root, child, remaining, expected)
		}
		return nil
	})
}

type inventoryDirectory interface {
	ReadDir(int) ([]fs.DirEntry, error)
}

// Reserve a batch before descending into children. At most one entry beyond
// the shared budget is read to distinguish a full directory from overflow.
func readInventoryEntries(directory inventoryDirectory, remaining *int, visit func(fs.DirEntry) error) error {
	for {
		entries, err := directory.ReadDir(min(inventoryReadBatch, *remaining+1))
		if err != nil && !errors.Is(err, io.EOF) {
			return errors.New("snapshot_directory_read_failed")
		}
		if len(entries) > *remaining {
			return errors.New("snapshot_entry_limit_exceeded")
		}
		*remaining -= len(entries)
		for _, entry := range entries {
			if visitErr := visit(entry); visitErr != nil {
				return visitErr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if len(entries) == 0 {
			return errors.New("snapshot_directory_no_progress")
		}
	}
}
