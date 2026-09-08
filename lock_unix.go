//go:build !windows

package chaintrail

import (
	"fmt"
	"os"
	"syscall"
)

type fileLock struct {
	file *os.File
}

func acquireFileLock(path string, exclusive bool) (*fileLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o640)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	mode := syscall.LOCK_SH
	if exclusive {
		mode = syscall.LOCK_EX
	}
	if err := syscall.Flock(int(file.Fd()), mode); err != nil {
		file.Close()
		return nil, fmt.Errorf("lock journal: %w", err)
	}
	return &fileLock{file: file}, nil
}

func (l *fileLock) Close() error {
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
