//go:build windows

package chaintrail

import "fmt"

type fileLock struct{}

func acquireFileLock(path string, exclusive bool) (*fileLock, error) {
	return nil, fmt.Errorf("journal locking is not supported on Windows")
}

func (l *fileLock) Close() error { return nil }
