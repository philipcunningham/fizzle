// Command corpus installs the versioned real-hardware test corpus.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	corpusURL = "https://github.com/philipcunningham/fizzle/releases/download/corpus-v1/fizzle-corpus-v1.tar.gz"
	corpusSHA = "2b070f5811c4a135161710202c7dea82dd99245653d851fb45ec40fd46c75b86"
)

func main() {
	destination := flag.String("destination", "testdata", "directory that will contain corpus/")
	cache := flag.String("cache", defaultCache(), "download cache directory")
	url := flag.String("url", corpusURL, "corpus archive URL")
	expected := flag.String("sha256", corpusSHA, "expected archive SHA-256")
	flag.Parse()
	if err := run(*destination, *cache, *url, *expected); err != nil {
		fmt.Fprintln(os.Stderr, "corpus:", err)
		os.Exit(1)
	}
}

func defaultCache() string {
	if root, err := os.UserCacheDir(); err == nil {
		return filepath.Join(root, "fizzle")
	}
	return filepath.Join(os.TempDir(), "fizzle-cache")
}

func run(destination, cache, url, expected string) error {
	root := filepath.Join(destination, "corpus")
	if corpusReady(root, expected) {
		return nil
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return err
	}
	archive := filepath.Join(cache, "fizzle-corpus-v1.tar.gz")
	if err := verifyFile(archive, expected); err != nil {
		if err := download(url, archive); err != nil {
			return fmt.Errorf("download %s: %w", url, err)
		}
		if err := verifyFile(archive, expected); err != nil {
			return err
		}
	}
	if err := extractFile(archive, destination); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, ".archive-sha256"), []byte(expected+"\n"), 0o644)
}

func corpusReady(root, expected string) bool {
	marker, err := os.ReadFile(filepath.Join(root, ".archive-sha256")) //nolint:gosec // fixed file below caller-selected root.
	return err == nil && strings.TrimSpace(string(marker)) == expected
}

func download(url, path string) error {
	response, err := http.Get(url) //nolint:gosec,noctx // URL is pinned and the CLI is intentionally cancellable by process signal.
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status %s", response.Status)
	}
	tmp := path + ".part"
	file, err := os.Create(tmp) //nolint:gosec // cache path is selected by the caller.
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, response.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp, path)
}

func verifyFile(path, expected string) error {
	file, err := os.Open(path) //nolint:gosec // cache path is selected by the caller.
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return err
	}
	got := hex.EncodeToString(digest.Sum(nil))
	if got != expected {
		return fmt.Errorf("archive SHA-256 = %s, want %s", got, expected)
	}
	return nil
}

func extractFile(path, destination string) error {
	file, err := os.Open(path) //nolint:gosec // verified archive path is selected by the caller.
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	return extractTar(tar.NewReader(gz), destination)
}

func extractTar(reader *tar.Reader, destination string) error {
	root, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(header.Name))
		if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
			return fmt.Errorf("archive path %q escapes destination", header.Name)
		}
		// The repository owns the installation instructions. Older archives
		// may contain a README, but installing fixtures must not rewrite it.
		if filepath.ToSlash(header.Name) == "corpus/README.md" {
			continue
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644) //nolint:gosec // target is contained above.
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(output, reader)
			closeErr := output.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("unsupported archive entry %q", header.Name)
		}
	}
}
