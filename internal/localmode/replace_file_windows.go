//go:build windows

package localmode

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

var replaceFileW = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

func replaceFileContents(path string, contents []byte) error {
	temporary, err := writeSyncedTemporary(filepath.Dir(path), contents)
	if err != nil {
		return err
	}
	defer os.Remove(temporary)
	targetPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	temporaryPtr, err := windows.UTF16PtrFromString(temporary)
	if err != nil {
		return err
	}
	result, _, callErr := replaceFileW.Call(uintptr(unsafe.Pointer(targetPtr)), uintptr(unsafe.Pointer(temporaryPtr)), 0, 1, 0, 0)
	if result != 0 {
		return nil
	}
	if err := rewriteProtectedFile(path, contents); err != nil {
		return fmt.Errorf("replace hosts file atomically (%v), then rewrite in place: %w", callErr, err)
	}
	return nil
}

func rewriteProtectedFile(path string, contents []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	written, err := file.WriteAt(contents, 0)
	if err != nil {
		return err
	}
	if written != len(contents) {
		return io.ErrShortWrite
	}
	if err := file.Truncate(int64(len(contents))); err != nil {
		return err
	}
	return file.Sync()
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
