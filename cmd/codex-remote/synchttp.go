package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"codex-runner/internal/codexremote/client"
	"codex-runner/internal/shared/jsonutil"
)

func syncPushViaDaemon(cl *client.Client, src, dst string, excludes []string) error {
	// Create tar.gz of source directory
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	src = filepath.Clean(src)
	err := filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		// Check excludes
		for _, pattern := range excludes {
			if matched, _ := filepath.Match(pattern, fi.Name()); matched {
				if fi.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		hdr, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return err
		}
		hdr.Name = rel

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		if fi.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			if _, err := io.Copy(tw, f); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create tar: %w", err)
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gw.Close(); err != nil {
		return err
	}

	ctx := context.Background()
	if err := cl.SyncUpload(ctx, dst, true, &buf); err != nil {
		return err
	}

	_ = jsonutil.WriteJSON(os.Stdout, map[string]any{
		"ok":   true,
		"mode": "push",
		"via":  "daemon",
		"src":  src,
		"dst":  dst,
	})
	return nil
}

func syncPullViaDaemon(cl *client.Client, src, dst string, excludes []string) error {
	ctx := context.Background()

	var buf bytes.Buffer
	if err := cl.SyncDownload(ctx, src, excludes, &buf); err != nil {
		return err
	}

	// Extract tar.gz to local destination
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}

	gr, err := gzip.NewReader(&buf)
	if err != nil {
		return fmt.Errorf("invalid gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	filesWritten := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("invalid tar: %w", err)
		}

		target := filepath.Join(dst, hdr.Name)
		// Security: prevent path traversal
		rel, err := filepath.Rel(dst, target)
		if err != nil || len(rel) > 1 && rel[:2] == ".." {
			return fmt.Errorf("path traversal detected: %s", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return err
			}
			_ = f.Close()
			filesWritten++
		case tar.TypeSymlink:
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
			filesWritten++
		}
	}

	_ = jsonutil.WriteJSON(os.Stdout, map[string]any{
		"ok":            true,
		"mode":          "pull",
		"via":           "daemon",
		"src":           src,
		"dst":           dst,
		"files_written": filesWritten,
	})
	return nil
}
