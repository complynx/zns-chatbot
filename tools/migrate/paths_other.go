//go:build !windows

package migrate

import "os"

func forbiddenLink(info os.FileInfo) bool { return info.Mode()&os.ModeSymlink != 0 }
