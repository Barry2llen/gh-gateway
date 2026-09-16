package localmode

import (
	"context"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakePlatform struct{ elevated bool }

func (fakePlatform) Supported() bool       { return true }
func (p fakePlatform) Elevated() bool      { return p.elevated }
func (fakePlatform) Lock() (func(), error) { return func() {}, nil }
func (fakePlatform) Restrict(string) error { return nil }

type memoryStateStore struct {
	value  State
	exists bool
	saves  []State
	dir    string
}

func (s *memoryStateStore) Load() (State, error) {
	if !s.exists {
		return State{}, ErrStateNotFound
	}
	return s.value, nil
}
func (s *memoryStateStore) Save(value State) error {
	s.value, s.exists = value, true
	s.saves = append(s.saves, value)
	return nil
}
func (s *memoryStateStore) Delete() error   { s.exists = false; return nil }
func (s *memoryStateStore) BaseDir() string { return s.dir }

type fakeHosts struct{ installed, removed bool }

func (h *fakeHosts) Inspect(string, string) (hostsInspection, error) {
	return hostsInspection{Own: h.installed}, nil
}
func (h *fakeHosts) Install(string, string) error { h.installed = true; return nil }
func (h *fakeHosts) Remove(string) error          { h.installed, h.removed = false, true; return nil }

type fakeCertificates struct{}

func (fakeCertificates) Ensure(string) (certInfo, error) {
	return certInfo{Thumbprint: "ABC", CertDir: "certs", Installed: true}, nil
}
func (fakeCertificates) Trusted(string) (bool, error) { return true, nil }
func (fakeCertificates) RemoveTrust(string) error     { return nil }

type fakeDocker struct{ started, removed bool }

func (d *fakeDocker) Check(context.Context) error { return nil }
func (d *fakeDocker) Start(context.Context, containerSpec) (string, error) {
	d.started = true
	return "container-id", nil
}
func (d *fakeDocker) Inspect(context.Context, string, int, bool) (containerInfo, error) {
	return containerInfo{Exists: d.started && !d.removed, Running: d.started && !d.removed, HTTPSPublished: true, SSHPublished: true, CertsReadOnly: true, VolumeExists: d.started && !d.removed}, nil
}
func (d *fakeDocker) Remove(context.Context, string) error { d.removed = true; return nil }

type fakeResolver struct{}

func (fakeResolver) Resolve(context.Context, string) (string, error) { return "10.0.0.20", nil }

type fakePorts struct{}

func (fakePorts) Available(int) error { return nil }

type failingFinalProbe struct{ calls int }

func (p *failingFinalProbe) WaitLocal(context.Context, string) error  { return nil }
func (p *failingFinalProbe) LocalHTTPS(context.Context, string) error { return nil }
func (p *failingFinalProbe) SystemHTTPS(context.Context, string, string) error {
	p.calls++
	return errors.New("final validation failed")
}
func (*failingFinalProbe) GraphQL(context.Context, string) error             { return nil }
func (*failingFinalProbe) Ordinary(context.Context, string) error            { return nil }
func (*failingFinalProbe) SSH(context.Context, int) error                    { return nil }
func (*failingFinalProbe) UpstreamTLS(context.Context, string, string) error { return nil }

type healthyProbe struct{}

func (healthyProbe) WaitLocal(context.Context, string) error           { return nil }
func (healthyProbe) LocalHTTPS(context.Context, string) error          { return nil }
func (healthyProbe) SystemHTTPS(context.Context, string, string) error { return nil }
func (healthyProbe) GraphQL(context.Context, string) error             { return nil }
func (healthyProbe) Ordinary(context.Context, string) error            { return nil }
func (healthyProbe) SSH(context.Context, int) error                    { return nil }
func (healthyProbe) UpstreamTLS(context.Context, string, string) error { return nil }

func TestStartRollsBackHostsAndContainerWhenFinalValidationFails(t *testing.T) {
	state, hosts, docker, probe := &memoryStateStore{}, &fakeHosts{}, &fakeDocker{}, &failingFinalProbe{}
	manager := &Manager{platform: fakePlatform{elevated: true}, state: state, hosts: hosts, certificates: fakeCertificates{}, docker: docker, resolver: fakeResolver{}, ports: fakePorts{}, probe: probe}
	_, err := manager.Start(context.Background(), StartOptions{Host: "git.example.com", Image: "local:test", SSHPort: 22, SSHProxy: true})
	if err == nil {
		t.Fatal("Start succeeded")
	}
	if !hosts.removed || !docker.removed {
		t.Fatalf("rollback hosts=%v docker=%v", hosts.removed, docker.removed)
	}
	if state.value.Active || state.value.HostsInstalled || state.value.ContainerID != "" {
		t.Fatalf("rolled back state = %+v", state.value)
	}
}

func TestStartRequiresElevationBeforeSideEffects(t *testing.T) {
	manager := &Manager{platform: fakePlatform{}}
	_, err := manager.Start(context.Background(), StartOptions{Host: "git.example.com", SSHPort: 22, SSHProxy: true})
	if err == nil || err.Error() != elevationError {
		t.Fatalf("error = %v", err)
	}
}

func TestStatusDetectsRunningAndInconsistentState(t *testing.T) {
	dir := t.TempDir()
	ca, caKey, err := generateCA(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	leaf, _, err := generateLeaf("git.example.com", ca, caKey, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "certs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "certs", "git.example.com.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf}), 0o600); err != nil {
		t.Fatal(err)
	}
	state := State{Version: 1, Active: true, Host: "git.example.com", InstanceID: "id", ContainerName: "container", SSHPort: 22, SSHProxy: true, CAThumbprint: "ABC", CAInstalled: true}
	store, hosts, docker := &memoryStateStore{value: state, exists: true, dir: dir}, &fakeHosts{installed: true}, &fakeDocker{started: true}
	manager := &Manager{state: store, hosts: hosts, certificates: fakeCertificates{}, docker: docker, probe: healthyProbe{}}
	report, err := manager.Status(context.Background())
	if err != nil || report.Condition != "running" {
		t.Fatalf("running report = %+v err=%v", report, err)
	}
	docker.removed = true
	report, err = manager.Status(context.Background())
	if err == nil || report.Condition != "inconsistent" {
		t.Fatalf("inconsistent report = %+v err=%v", report, err)
	}
}
