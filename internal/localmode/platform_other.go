//go:build !windows

package localmode

import "errors"

type systemPlatform struct{}

func (systemPlatform) Supported() bool       { return false }
func (systemPlatform) Elevated() bool        { return false }
func (systemPlatform) Lock() (func(), error) { return func() {}, nil }
func (systemPlatform) Restrict(string) error { return nil }

type commandTrustStore struct{}

func (commandTrustStore) Trusted(string) (bool, error) {
	return false, errors.New("Windows certificate store is unavailable")
}
func (commandTrustStore) Install(string) error {
	return errors.New("Windows certificate store is unavailable")
}
func (commandTrustStore) Remove(string) error {
	return errors.New("Windows certificate store is unavailable")
}
