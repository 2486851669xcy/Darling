package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSafeStaticFileSystemRejectsEscapingSymlink(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	privateDir := filepath.Join(baseDir, "private")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("create data directory: %v", err)
	}
	if err := os.MkdirAll(privateDir, 0700); err != nil {
		t.Fatalf("create private directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "public.txt"), []byte("public"), 0644); err != nil {
		t.Fatalf("write public fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(privateDir, "dimension.db"), []byte("private"), 0600); err != nil {
		t.Fatalf("write private fixture: %v", err)
	}
	if err := os.Symlink(privateDir, filepath.Join(dataDir, "leak")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("Windows user cannot create symlink fixture: %v", err)
		}
		t.Fatalf("create escaping symlink: %v", err)
	}

	staticFS, err := newSafeStaticFileSystem(dataDir)
	if err != nil {
		t.Fatalf("create safe static filesystem: %v", err)
	}
	publicFile, err := staticFS.Open("/public.txt")
	if err != nil {
		t.Fatalf("open regular public file: %v", err)
	}
	_ = publicFile.Close()

	escapedFile, err := staticFS.Open("/leak/dimension.db")
	if escapedFile != nil {
		_ = escapedFile.Close()
		t.Fatal("safe static filesystem opened a file outside DATA_DIR")
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("escaping symlink returned %v, want permission error", err)
	}
}
