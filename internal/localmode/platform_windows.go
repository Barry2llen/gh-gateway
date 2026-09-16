//go:build windows

package localmode

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

type systemPlatform struct{}

func (systemPlatform) Supported() bool {
	version := windows.RtlGetVersion()
	return version.MajorVersion > 10 || (version.MajorVersion == 10 && version.BuildNumber >= 22000)
}
func (systemPlatform) Elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }
func (systemPlatform) Lock() (func(), error) {
	name, err := windows.UTF16PtrFromString(`Local\gh-gateway-localmode`)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		return nil, err
	}
	result, err := windows.WaitForSingleObject(handle, 30000)
	if err != nil || (result != windows.WAIT_OBJECT_0 && result != windows.WAIT_ABANDONED) {
		windows.CloseHandle(handle)
		return nil, errors.New("another gh-gateway local mode command is running")
	}
	return func() { _ = windows.ReleaseMutex(handle); _ = windows.CloseHandle(handle) }, nil
}
func (systemPlatform) Restrict(path string) error {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return err
	}
	identity := "*" + user.User.Sid.String() + ":(F)"
	output, err := exec.Command("icacls.exe", path, "/inheritance:r", "/grant:r", identity).CombinedOutput()
	if err != nil {
		return fmt.Errorf("restrict private key ACL: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

type commandTrustStore struct{}

func (commandTrustStore) Trusted(thumbprint string) (bool, error) {
	if strings.TrimSpace(thumbprint) == "" {
		return false, errors.New("certificate thumbprint is empty")
	}
	output, err := exec.CommandContext(context.Background(), "certutil.exe", "-user", "-store", "Root", thumbprint).CombinedOutput()
	if err != nil {
		return false, nil
	}
	return strings.Contains(strings.ToUpper(strings.ReplaceAll(string(output), " ", "")), strings.ToUpper(thumbprint)), nil
}
func (commandTrustStore) Install(path string) error {
	output, err := exec.Command("certutil.exe", "-user", "-addstore", "Root", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("certutil addstore: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
func (commandTrustStore) Remove(thumbprint string) error {
	output, err := exec.Command("certutil.exe", "-user", "-delstore", "Root", thumbprint).CombinedOutput()
	if err != nil && !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return fmt.Errorf("certutil delstore: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
