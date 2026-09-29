//go:build windows

package migrate

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type reparseInfo struct{ attributes uint32 }

func (reparseInfo) Name() string       { return "junction" }
func (reparseInfo) Size() int64        { return 0 }
func (reparseInfo) Mode() os.FileMode  { return os.ModeDir }
func (reparseInfo) ModTime() time.Time { return time.Time{} }
func (reparseInfo) IsDir() bool        { return true }
func (r reparseInfo) Sys() any         { return &syscall.Win32FileAttributeData{FileAttributes: r.attributes} }

func TestWindowsReparsePointsRejectedWithoutSymlinkMode(t *testing.T) {
	t.Parallel()
	assert.True(t, forbiddenLink(reparseInfo{attributes: syscall.FILE_ATTRIBUTE_REPARSE_POINT}))
	assert.False(t, forbiddenLink(reparseInfo{attributes: syscall.FILE_ATTRIBUTE_DIRECTORY}))
}
