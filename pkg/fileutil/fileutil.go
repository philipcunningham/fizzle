// Package fileutil provides shared file utilities used across fizzle packages.
package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	lockAttempts      = 50
	lockRetryInterval = 100 * time.Millisecond
	// staleLockThreshold allows slow disk operations while recovering locks from dead processes.
	staleLockThreshold = 5 * time.Minute
)

// WithFileLock runs fn under an exclusive lock and recovers stale locks.
func WithFileLock(path string, fn func() error) error {
	lockPath := path + ".lock"
	for range lockAttempts {
		lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			defer func() {
				lf.Close()          //nolint:errcheck
				os.Remove(lockPath) //nolint:errcheck
			}()
			return fn()
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > staleLockThreshold {
			os.Remove(lockPath) //nolint:errcheck // best-effort; loser of the race just retries
			continue
		}
		time.Sleep(lockRetryInterval)
	}
	return fmt.Errorf("fileutil: could not acquire lock on %s (another fizzle process may be using it; if none is running, remove %s manually)", path, lockPath)
}

// WriteAtomic replaces path through a same-directory temporary file.
func WriteAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("fileutil: creating directory %q: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "fizzle-*")
	if err != nil {
		return fmt.Errorf("fileutil: creating temp file in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()        //nolint:errcheck
		os.Remove(tmpName) //nolint:errcheck
		return fmt.Errorf("fileutil: writing %q: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()        //nolint:errcheck
		os.Remove(tmpName) //nolint:errcheck
		return fmt.Errorf("fileutil: syncing %q: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName) //nolint:errcheck
		return fmt.Errorf("fileutil: closing temp file: %w", err)
	}
	if err := os.Chmod(tmpName, 0644); err != nil {
		os.Remove(tmpName) //nolint:errcheck
		return fmt.Errorf("fileutil: setting permissions on %q: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName) //nolint:errcheck
		return fmt.Errorf("fileutil: writing %q: %w", path, err)
	}
	return nil
}
