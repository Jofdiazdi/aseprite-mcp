package asepritemcp

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Workspace struct {
	path string
	root *os.Root
}

func (w *Workspace) Close() error { return w.root.Close() }

func OpenWorkspace(path string) (*Workspace, error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return nil, fmt.Errorf("workspace must exist: %w", err)
	}
	r, err := os.OpenRoot(p)
	if err != nil {
		return nil, err
	}
	return &Workspace{p, r}, nil
}

// Resolve checks existing parents as well as the leaf: a new file through a
// symlinked directory must not escape the workspace either.
func (w *Workspace) Resolve(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(w.path, path)
	}
	path = filepath.Clean(path)
	probe := path
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(probe)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			if _, err := w.relative(resolved); err != nil {
				return "", err
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", fmt.Errorf("cannot resolve path")
		}
		tail = append(tail, filepath.Base(probe))
		probe = parent
	}
}

func (w *Workspace) relative(path string) (string, error) {
	r, err := filepath.Rel(w.path, path)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) {
		return "", fmt.Errorf("path is outside the configured workspace")
	}
	return r, nil
}

func (w *Workspace) Hash(path string) (string, error) {
	r, err := w.relative(path)
	if err != nil {
		return "", err
	}
	f, err := w.root.Open(r)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func (w *Workspace) Write(path string, data []byte) error {
	r, err := w.relative(path)
	if err != nil {
		return err
	}
	name := filepath.Join(filepath.Dir(r), ".aseprite-mcp-"+randomID())
	f, err := w.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer w.root.Remove(name)
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	return w.root.Rename(name, r)
}

func (w *Workspace) Read(path string) ([]byte, error) {
	r, err := w.relative(path)
	if err != nil {
		return nil, err
	}
	return w.root.ReadFile(r)
}
