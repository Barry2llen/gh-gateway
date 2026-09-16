package localmode

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrStateNotFound = errors.New("gh-gateway local state not found")

type State struct {
	Version        int    `json:"version"`
	Active         bool   `json:"active"`
	InstanceID     string `json:"instance_id"`
	Host           string `json:"host"`
	UpstreamIP     string `json:"upstream_ip"`
	HTTPSPort      int    `json:"https_port"`
	SSHPort        int    `json:"ssh_port"`
	SSHProxy       bool   `json:"ssh_proxy"`
	ContainerName  string `json:"container_name"`
	ContainerID    string `json:"container_id"`
	CertVolume     string `json:"cert_volume"`
	Image          string `json:"image"`
	HostsInstalled bool   `json:"hosts_installed"`
	CAThumbprint   string `json:"ca_thumbprint"`
	CAInstalled    bool   `json:"ca_installed"`
}

type stateStore interface {
	Load() (State, error)
	Save(State) error
	Delete() error
	BaseDir() string
}

type fileStateStore struct{ baseDir string }

func (s fileStateStore) BaseDir() string { return s.baseDir }

func (s fileStateStore) Load() (State, error) {
	data, err := os.ReadFile(filepath.Join(s.baseDir, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return State{}, ErrStateNotFound
	}
	if err != nil {
		return State{}, fmt.Errorf("read state: %w", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("decode state: %w", err)
	}
	if state.Version != 1 {
		return State{}, fmt.Errorf("unsupported state version %d", state.Version)
	}
	return state, nil
}

func (s fileStateStore) Save(state State) error {
	if err := os.MkdirAll(s.baseDir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	state.Version = 1
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(s.baseDir, "state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary state: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close state: %w", err)
	}
	if err := os.Rename(temporaryName, filepath.Join(s.baseDir, "state.json")); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}

func (s fileStateStore) Delete() error {
	if err := os.RemoveAll(s.baseDir); err != nil {
		return fmt.Errorf("delete local state: %w", err)
	}
	return nil
}
