//go:build !windows

package localmode

import (
	"fmt"
	"os"
	"path/filepath"
)

func replaceFileContents(path string, contents []byte) error {
	temporary, err := writeSyncedTemporary(filepath.Dir(path), contents)
	if err != nil {
		return err
	}
	defer os.Remove(temporary)
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("replace hosts file: %w", err)
	}
	return nil
}

func writeSyncedTemporary(directory string, contents []byte) (string, error) {
	temporary, err := os.CreateTemp(directory, ".gh-gateway-hosts-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temporary hosts file: %w", err)
	}
	name := temporary.Name()
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		os.Remove(name)
		return "", err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		os.Remove(name)
		return "", err
	}
	if err := temporary.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}
