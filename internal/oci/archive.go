package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CreateTarGzip creates a tar.gz archive of the given directory.
// It skips .git and dist directories and common non-essential files.
func CreateTarGzip(dir string) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		if relPath == "." {
			return nil
		}

		base := filepath.Base(relPath)
		if info.IsDir() && (base == ".git" || base == "dist") {
			return filepath.SkipDir
		}

		if !info.IsDir() && shouldSkipFile(base) {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		header.Name = filepath.ToSlash(relPath)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if !info.IsDir() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(tw, f)
			f.Close() // Close immediately, not via defer (prevents FD leak in Walk loop)
			if copyErr != nil {
				return copyErr
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func shouldSkipFile(name string) bool {
	skip := []string{
		".DS_Store",
		"Thumbs.db",
		".gitignore",
	}
	lower := strings.ToLower(name)
	for _, s := range skip {
		if lower == strings.ToLower(s) {
			return true
		}
	}
	return false
}
