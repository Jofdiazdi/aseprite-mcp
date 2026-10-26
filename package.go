package asepritemcp

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
)

//go:generate go run ./cmd/package --sync

// PackageExtension builds the native extension from the same embedded Lua used
// by batch mode. syncDir optionally updates the source extension in this repo.
func PackageExtension(output, syncDir string) error {
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	f, err := os.Create(output)
	if err != nil {
		return err
	}
	z := zip.NewWriter(f)
	for _, name := range []string{"package.json", "plugin.lua", "dispatcher.lua"} {
		data, err := assets.ReadFile("assets/" + name)
		if err != nil {
			z.Close()
			f.Close()
			return err
		}
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0644)
		entry, err := z.CreateHeader(header)
		if err == nil {
			_, err = entry.Write(data)
		}
		if err != nil {
			z.Close()
			f.Close()
			return err
		}
		if syncDir != "" {
			if err := os.MkdirAll(syncDir, 0755); err != nil {
				z.Close()
				f.Close()
				return err
			}
			if err := os.WriteFile(filepath.Join(syncDir, name), data, 0644); err != nil {
				z.Close()
				f.Close()
				return err
			}
		}
	}
	if err := z.Close(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return nil
}

func PackagePath(path string) string {
	if path == "" {
		return filepath.Join("dist", "agent-mcp.aseprite-extension")
	}
	return path
}

func ValidateMode(mode string) error {
	if mode != "live" && mode != "batch" {
		return fmt.Errorf("mode must be live or batch")
	}
	return nil
}
