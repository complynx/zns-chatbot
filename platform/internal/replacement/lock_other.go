//go:build !linux

package replacement

// Lock rejects unsupported deployment hosts. Policy and journal tests remain portable.
func Lock(string) (func() error, error) { return nil, ErrConfiguration }

func syncDirectory(string) error { return nil }
