package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type safeStaticFileSystem struct {
	rootDir string
}

func newSafeStaticFileSystem(rootDir string) (http.FileSystem, error) {
	rootAbs, err := filepath.Abs(strings.TrimSpace(rootDir))
	if err != nil {
		return nil, fmt.Errorf("resolve DATA_DIR: %w", err)
	}
	rootCanonical, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return nil, fmt.Errorf("resolve DATA_DIR symlinks: %w", err)
	}
	info, err := os.Stat(rootCanonical)
	if err != nil {
		return nil, fmt.Errorf("inspect DATA_DIR: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("DATA_DIR must be a directory")
	}
	return &safeStaticFileSystem{rootDir: rootCanonical}, nil
}

func (f *safeStaticFileSystem) Open(name string) (http.File, error) {
	cleaned := path.Clean("/" + name)
	relative := strings.TrimPrefix(cleaned, "/")
	candidate := filepath.Join(f.rootDir, filepath.FromSlash(relative))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return nil, err
	}
	if !pathIsWithin(f.rootDir, resolved) {
		return nil, fs.ErrPermission
	}
	return os.Open(resolved)
}
