package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWriteAtomicCreatesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "out.bin")
	data := []byte("hello fizzle")

	if err := WriteAtomic(path, data); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Errorf("got %q, want %q", got, data)
	}
}

func TestWriteAtomicOverwritesExisting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "out.bin")

	if err := WriteAtomic(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("second")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Errorf("got %q, want %q", got, "second")
	}
}

func TestWriteAtomicCreatesIntermediateDirectories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "out.bin")

	if err := WriteAtomic(path, []byte("data")); err != nil {
		t.Fatalf("WriteAtomic with nested path: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file at %s: %v", path, err)
	}
}

func TestWriteAtomicEmptyData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.bin")

	if err := WriteAtomic(path, []byte{}); err != nil {
		t.Fatalf("WriteAtomic empty: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Errorf("expected empty file, got size %d", info.Size())
	}
}

func TestWriteAtomicNoTempFileLeftOnDisk(t *testing.T) {
	t.Parallel()
	// After a successful write there should be exactly one file in the dir.
	dir := t.TempDir()
	path := filepath.Join(dir, "out.bin")

	if err := WriteAtomic(path, []byte("data")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("expected 1 file, got %d: %v", len(entries), names)
	}
}

func TestWriteAtomicPermissions(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions not supported on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "perms.bin")
	if err := WriteAtomic(path, []byte("data")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0644 {
		t.Errorf("permissions: got %o, want 0644", perm)
	}
}

func TestWriteAtomicLargeData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "large.bin")
	data := make([]byte, 1024*1024) // 1 MB
	for i := range data {
		data[i] = byte(i % 251)
	}

	if err := WriteAtomic(path, data); err != nil {
		t.Fatalf("WriteAtomic large: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(data) {
		t.Errorf("size: got %d, want %d", len(got), len(data))
	}
	for i, b := range data {
		if got[i] != b {
			t.Errorf("byte %d: got %d, want %d", i, got[i], b)
			break
		}
	}
}

func TestWriteAtomicBadParentPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("I am a file"), 0644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(blocker, "subdir", "out.bin")
	err := WriteAtomic(path, []byte("data"))
	if err == nil {
		t.Fatal("expected error when parent path is a file")
	}
}

func TestWriteAtomicReadOnlyDir(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("read-only directory via chmod not enforced on Windows")
	}
	dir := t.TempDir()
	roDir := filepath.Join(dir, "readonly")
	if err := os.Mkdir(roDir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0755) })
	path := filepath.Join(roDir, "out.bin")
	err := WriteAtomic(path, []byte("data"))
	if err == nil {
		t.Fatal("expected error writing to read-only directory")
	}
}

func TestWithFileLockRunsFn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "target.bin")

	ran := false
	err := WithFileLock(path, func() error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithFileLock: %v", err)
	}
	if !ran {
		t.Error("fn was not called")
	}
}

func TestWithFileLockPropagatesFnError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "target.bin")

	want := errors.New("fn failed")
	err := WithFileLock(path, func() error {
		return want
	})
	if !errors.Is(err, want) {
		t.Errorf("got %v, want %v", err, want)
	}
}

func TestWithFileLockCleansUpLockFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "target.bin")

	err := WithFileLock(path, func() error {
		if _, err := os.Stat(path + ".lock"); err != nil {
			t.Error("lock file should exist during fn")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Error("lock file should be removed after fn returns")
	}
}

func TestWithFileLockCleansUpOnError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "target.bin")

	_ = WithFileLock(path, func() error {
		return errors.New("fail")
	})
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Error("lock file should be removed even when fn returns error")
	}
}

func TestWithFileLockMutualExclusion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "target.bin")

	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32
	var wg sync.WaitGroup

	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = WithFileLock(path, func() error {
				cur := concurrent.Add(1)
				for {
					old := maxConcurrent.Load()
					if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
				concurrent.Add(-1)
				return nil
			})
		}()
	}
	wg.Wait()

	if maxConcurrent.Load() > 1 {
		t.Errorf("max concurrent = %d, want 1 (lock did not provide mutual exclusion)", maxConcurrent.Load())
	}
}

func TestWithFileLockTimeoutOnStaleLock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "target.bin")
	lockPath := path + ".lock"

	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	lf.Close() //nolint:errcheck
	t.Cleanup(func() { _ = os.Remove(lockPath) })

	start := time.Now()
	err = WithFileLock(path, func() error {
		return nil
	})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error when lock file already exists")
	}
	if !strings.Contains(err.Error(), "could not acquire lock") {
		t.Errorf("unexpected error: %v", err)
	}
	if elapsed < 4*time.Second {
		t.Errorf("expected timeout to take at least 4s, got %v", elapsed)
	}
}
