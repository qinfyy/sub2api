//go:build windows

package repository

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func tryLockInstanceFile(file *os.File) (bool, error) {
	// Reserve a byte beyond EOF so the informational PID remains readable.
	overlapped := windows.Overlapped{OffsetHigh: 1}
	err := windows.LockFileEx(windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &overlapped)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}
