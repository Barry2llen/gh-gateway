package localmode

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const DefaultImage = "ghcr.io/barry2llen/gh-gateway:latest"
const elevationError = "This command must be run from an elevated terminal."

var hostnamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

type platform interface {
	Supported() bool
	Elevated() bool
	Lock() (func(), error)
	Restrict(string) error
}

type Manager struct {
	platform     platform
	state        stateStore
	hosts        hostsManager
	certificates certificateManager
	docker       dockerRuntime
	resolver     resolver
	ports        portChecker
	probe        prober
}

type StartOptions struct {
	Host, Image string
	SSHPort     int
	SSHProxy    bool
}
type StartResult struct {
	State          State
	AlreadyRunning bool
	Warning        string
}

func (r StartResult) String() string {
	if r.AlreadyRunning {
		return "gh-gateway is already running for " + r.State.Host + ".\n"
	}
	ssh := "disabled"
	if r.State.SSHProxy {
		ssh = fmt.Sprintf("127.0.0.1:%d -> %s:%d", r.State.SSHPort, r.State.UpstreamIP, r.State.SSHPort)
	}
	text := fmt.Sprintf("Resolved upstream\n  %s -> %s\n\nTLS\n  local CA trusted\n  certificate ready\n\nNetwork\n  HTTPS 127.0.0.1:443\n  SSH   %s\n\nContainer\n  %s running\n\nHosts\n  %s -> 127.0.0.1\n\nReady.\n\nPowerShell:\n  $env:GH_HOST=\"%s\"\n  $env:GH_ENTERPRISE_TOKEN=\"<Gitea PAT>\"\n", r.State.Host, r.State.UpstreamIP, ssh, r.State.ContainerName, r.State.Host, r.State.Host)
	if r.Warning != "" {
		text = "WARNING: " + r.Warning + "\n\n" + text
	}
	return text
}

func NewDefaultManager() *Manager {
	baseDir := filepath.Join(os.Getenv("LOCALAPPDATA"), "gh-gateway")
	if os.Getenv("LOCALAPPDATA") == "" {
		baseDir = filepath.Join(os.TempDir(), "gh-gateway")
	}
	hostsPath := filepath.Join(os.Getenv("SystemRoot"), "System32", "drivers", "etc", "hosts")
	platform := systemPlatform{}
	store := fileStateStore{baseDir: baseDir}
	trust := commandTrustStore{}
	return &Manager{platform: platform, state: store, hosts: fileHostsManager{path: hostsPath}, certificates: fileCertificateManager{baseDir: baseDir, trust: trust, restrict: platform.Restrict, now: time.Now}, docker: dockerCLI{runner: execRunner{}}, resolver: networkResolver{}, ports: tcpPortChecker{}, probe: networkProber{}}
}

func (m *Manager) Start(ctx context.Context, options StartOptions) (result StartResult, returnErr error) {
	if err := validateStartOptions(&options); err != nil {
		return result, err
	}
	if err := m.requireElevation(); err != nil {
		return result, err
	}
	unlock, err := m.platform.Lock()
	if err != nil {
		return result, err
	}
	defer unlock()

	existing, err := m.state.Load()
	if err == nil {
		report, _ := m.statusForState(ctx, existing)
		if existing.Active && report.Condition == "running" && existing.Host == options.Host {
			return StartResult{State: existing, AlreadyRunning: true}, nil
		}
		if report.Condition == "inconsistent" || existing.Active {
			return result, errors.New("an active or inconsistent local mode instance exists; run gh-gateway stop first")
		}
	}
	if err != nil && !errors.Is(err, ErrStateNotFound) {
		return result, err
	}
	if err := m.docker.Check(ctx); err != nil {
		return result, err
	}

	instanceID, err := newInstanceID()
	if err != nil {
		return result, err
	}
	inspection, err := m.hosts.Inspect(options.Host, instanceID)
	if err != nil {
		return result, err
	}
	if inspection.Conflict {
		return result, fmt.Errorf("host %s is already managed by another hosts entry", options.Host)
	}
	upstreamIP, err := m.resolver.Resolve(ctx, options.Host)
	if err != nil {
		return result, err
	}
	if err := m.ports.Available(443); err != nil {
		return result, err
	}
	if options.SSHProxy {
		if err := m.ports.Available(options.SSHPort); err != nil {
			return result, err
		}
	}

	certificate, err := m.certificates.Ensure(options.Host)
	if err != nil {
		return result, fmt.Errorf("prepare local TLS certificates: %w", err)
	}
	state := State{Version: 1, Active: false, InstanceID: instanceID, Host: options.Host, UpstreamIP: upstreamIP, HTTPSPort: 443, SSHPort: options.SSHPort, SSHProxy: options.SSHProxy, ContainerName: containerName(options.Host), Image: options.Image, CAThumbprint: certificate.Thumbprint, CAInstalled: certificate.Installed}
	state.CertVolume = certVolumeName(state.ContainerName)
	if err := m.state.Save(state); err != nil {
		return result, errors.Join(err, m.certificates.RemoveTrust(certificate.Thumbprint))
	}

	containerCreated, hostsAdded := false, false
	defer func() {
		if returnErr == nil {
			return
		}
		var rollback []error
		if hostsAdded {
			rollback = append(rollback, m.hosts.Remove(instanceID))
		}
		if containerCreated {
			rollback = append(rollback, m.docker.Remove(context.Background(), state.ContainerName))
		}
		state.Active, state.HostsInstalled, state.ContainerID = false, false, ""
		rollback = append(rollback, m.state.Save(state))
		returnErr = errors.Join(append([]error{returnErr}, rollback...)...)
	}()

	state.ContainerID, err = m.docker.Start(ctx, containerSpec{Name: state.ContainerName, InstanceID: instanceID, Host: options.Host, UpstreamIP: upstreamIP, Image: options.Image, LeafCert: certificate.LeafCert, LeafKey: certificate.LeafKey, CertVolume: state.CertVolume, SSHPort: options.SSHPort, SSHProxy: options.SSHProxy})
	if err != nil {
		return result, err
	}
	containerCreated = true
	if err := m.probe.WaitLocal(ctx, options.Host); err != nil {
		return result, err
	}
	if err := m.hosts.Install(options.Host, instanceID); err != nil {
		return result, err
	}
	hostsAdded, state.HostsInstalled = true, true
	if err := m.probe.SystemHTTPS(ctx, options.Host, "/api/v3/user"); err != nil {
		return result, fmt.Errorf("validate HTTPS through hosts override: %w", err)
	}
	state.Active = true
	if err := m.state.Save(state); err != nil {
		return result, err
	}
	warning := ""
	if !options.SSHProxy {
		warning = "SSH remotes using this hostname will not work while the hosts override is active."
	}
	return StartResult{State: state, Warning: warning}, nil
}

func validateStartOptions(options *StartOptions) error {
	options.Host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(options.Host), "."))
	if !hostnamePattern.MatchString(options.Host) || net.ParseIP(options.Host) != nil || options.Host == "localhost" {
		return errors.New("host must be a fully qualified ASCII DNS name")
	}
	if options.Image == "" {
		options.Image = DefaultImage
	}
	if options.SSHPort < 1 || options.SSHPort > 65535 {
		return errors.New("ssh port must be between 1 and 65535")
	}
	if options.SSHProxy && options.SSHPort == 443 {
		return errors.New("SSH port cannot be the HTTPS port 443")
	}
	return nil
}

func (m *Manager) requireElevation() error {
	if !m.platform.Supported() {
		return errors.New("Windows 11 is required for local transparent mode")
	}
	if !m.platform.Elevated() {
		return errors.New(elevationError)
	}
	return nil
}

func newInstanceID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}
func containerName(host string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(host)))
	return "gh-gateway-" + hex.EncodeToString(sum[:])[:6]
}

func (m *Manager) Stop(ctx context.Context) (string, error) {
	if err := m.requireElevation(); err != nil {
		return "", err
	}
	unlock, err := m.platform.Lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	return m.stopLocked(ctx)
}

func (m *Manager) stopLocked(ctx context.Context) (string, error) {
	state, err := m.state.Load()
	if errors.Is(err, ErrStateNotFound) {
		return "Status: stopped\n", nil
	}
	if err != nil {
		return "", err
	}
	var failures []error
	if err := m.docker.Remove(ctx, state.ContainerName); err != nil {
		failures = append(failures, err)
	}
	if err := m.hosts.Remove(state.InstanceID); err != nil {
		failures = append(failures, err)
	} else {
		state.HostsInstalled = false
	}
	state.Active, state.ContainerID = false, ""
	if err := m.state.Save(state); err != nil {
		failures = append(failures, err)
	}
	return "Status: stopped\n", errors.Join(failures...)
}

type Check struct {
	Name   string
	OK     bool
	Detail string
}
type Report struct {
	Title, Condition string
	Checks           []Check
}

func (r Report) String() string {
	var builder strings.Builder
	if r.Title != "" {
		builder.WriteString(r.Title)
		builder.WriteString("\n")
	}
	if r.Condition != "" {
		builder.WriteString("Status: ")
		builder.WriteString(r.Condition)
		builder.WriteString("\n")
	}
	for _, check := range r.Checks {
		symbol := "✓"
		if !check.OK {
			symbol = "✗"
		}
		fmt.Fprintf(&builder, "%s %s", symbol, check.Name)
		if check.Detail != "" {
			fmt.Fprintf(&builder, ": %s", check.Detail)
		}
		builder.WriteByte('\n')
	}
	return builder.String()
}

func (m *Manager) Status(ctx context.Context) (Report, error) {
	state, err := m.state.Load()
	if errors.Is(err, ErrStateNotFound) {
		return Report{Condition: "stopped"}, nil
	}
	if err != nil {
		return Report{Condition: "inconsistent", Checks: []Check{{Name: "state is readable", Detail: err.Error()}}}, err
	}
	return m.statusForState(ctx, state)
}

func (m *Manager) statusForState(ctx context.Context, state State) (Report, error) {
	report := Report{}
	inspection, hostsErr := m.hosts.Inspect(state.Host, state.InstanceID)
	container, dockerErr := m.docker.Inspect(ctx, state.ContainerName, state.SSHPort, state.SSHProxy)
	trusted, trustErr := m.certificates.Trusted(state.CAThumbprint)
	leafOK := validLeaf(filepath.Join(m.state.BaseDir(), "certs", state.Host+".crt"), state.Host, time.Now())
	var listenerErr error
	if state.Active {
		listenerErr = m.probe.LocalHTTPS(ctx, state.Host)
	}
	hostsOK := hostsErr == nil && inspection.Own == state.Active
	containerExistsOK := dockerErr == nil && container.Exists == state.Active
	containerRunningOK := dockerErr == nil && container.Running == state.Active
	httpsPublishedOK := dockerErr == nil && ((!state.Active && !container.Exists) || (state.Active && container.HTTPSPublished))
	sshPublishedOK := dockerErr == nil && ((!state.Active && !container.Exists) || (state.Active && container.SSHPublished))
	certMountOK := dockerErr == nil && ((!state.Active && !container.VolumeExists) || (state.Active && container.VolumeExists && container.CertsReadOnly))
	report.Checks = []Check{{Name: "state exists", OK: true}, {Name: "hosts override state matches", OK: hostsOK, Detail: errorDetail(hostsErr)}, {Name: "container existence matches", OK: containerExistsOK, Detail: errorDetail(dockerErr)}, {Name: "container running state matches", OK: containerRunningOK}, {Name: "HTTPS port state matches", OK: httpsPublishedOK}, {Name: "certificate volume is read-only", OK: certMountOK}, {Name: "local HTTPS listener", OK: !state.Active || listenerErr == nil, Detail: errorDetail(listenerErr)}, {Name: "SSH port state matches", OK: sshPublishedOK}, {Name: "local CA is trusted", OK: trustErr == nil && trusted, Detail: errorDetail(trustErr)}, {Name: "leaf certificate is valid", OK: leafOK}}
	if allChecks(report.Checks) {
		if state.Active {
			report.Condition = "running"
		} else {
			report.Condition = "stopped"
		}
		return report, nil
	}
	report.Condition = "inconsistent"
	return report, errors.New("local mode state is inconsistent")
}

func allChecks(checks []Check) bool {
	for _, check := range checks {
		if !check.OK {
			return false
		}
	}
	return true
}
func errorDetail(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (m *Manager) Doctor(ctx context.Context) (Report, error) {
	report := Report{Title: "gh-gateway doctor"}
	report.Checks = append(report.Checks, Check{Name: "Windows supported", OK: m.platform.Supported()}, Check{Name: "elevated terminal", OK: m.platform.Elevated()})
	report.Checks = append(report.Checks, checkError("Docker reachable", m.docker.Check(ctx)))
	state, err := m.state.Load()
	if errors.Is(err, ErrStateNotFound) {
		report.Checks = append(report.Checks, Check{Name: "state", OK: true, Detail: "not configured"})
		report.Checks = append(report.Checks, checkError("HTTPS port available", m.ports.Available(443)))
		report.Condition = "stopped"
		if !allRequiredDoctorChecks(report.Checks) {
			return report, m.doctorError()
		}
		return report, nil
	}
	if err != nil {
		report.Checks = append(report.Checks, Check{Name: "state readable", Detail: err.Error()})
		report.Condition = "inconsistent"
		return report, err
	}
	status, statusErr := m.statusForState(ctx, state)
	report.Checks = append(report.Checks, status.Checks...)
	if state.Active {
		report.Checks = append(report.Checks, checkError("recorded upstream TLS", m.probe.UpstreamTLS(ctx, state.Host, state.UpstreamIP)), checkError("HTTPS certificate validation", m.probe.SystemHTTPS(ctx, state.Host, "/api/v3/user")), checkError("/api/graphql reachable", m.probe.GraphQL(ctx, state.Host)), checkError("ordinary Gitea passthrough", m.probe.Ordinary(ctx, state.Host)))
		if state.SSHProxy {
			report.Checks = append(report.Checks, checkError("SSH passthrough", m.probe.SSH(ctx, state.SSHPort)))
		}
		if gh, lookupErr := exec.LookPath("gh"); lookupErr == nil {
			output, authErr := exec.CommandContext(ctx, gh, "auth", "status", "--hostname", state.Host).CombinedOutput()
			detail := strings.TrimSpace(string(output))
			if authErr != nil {
				report.Checks = append(report.Checks, Check{Name: "gh authentication", OK: true, Detail: "warning: " + detail})
			} else {
				report.Checks = append(report.Checks, Check{Name: "gh authentication", OK: true})
			}
		}
	} else if state.Host != "" {
		_, resolveErr := m.resolver.Resolve(ctx, state.Host)
		report.Checks = append(report.Checks, checkError("host DNS and upstream TLS", resolveErr))
		report.Checks = append(report.Checks, checkError("HTTPS port available", m.ports.Available(443)))
		if state.SSHProxy {
			report.Checks = append(report.Checks, checkError("SSH port available", m.ports.Available(state.SSHPort)))
		}
	}
	report.Condition = status.Condition
	if statusErr != nil || !allRequiredDoctorChecks(report.Checks) {
		return report, m.doctorError()
	}
	return report, nil
}

func checkError(name string, err error) Check {
	return Check{Name: name, OK: err == nil, Detail: errorDetail(err)}
}
func allRequiredDoctorChecks(checks []Check) bool {
	for _, check := range checks {
		if !check.OK {
			return false
		}
	}
	return true
}

func (m *Manager) doctorError() error {
	if !m.platform.Supported() {
		return errors.New("Windows 11 is required for local transparent mode")
	}
	if !m.platform.Elevated() {
		return errors.New(elevationError)
	}
	return errors.New("doctor found local mode problems")
}

func (m *Manager) Uninstall(ctx context.Context) (string, error) {
	if err := m.requireElevation(); err != nil {
		return "", err
	}
	unlock, err := m.platform.Lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	state, err := m.state.Load()
	if errors.Is(err, ErrStateNotFound) {
		return "gh-gateway local mode is not installed.\n", nil
	}
	if err != nil {
		return "", err
	}
	if _, err := m.stopLocked(ctx); err != nil {
		return "", err
	}
	if state.CAThumbprint == "" {
		return "", errors.New("state does not contain an exact CA thumbprint; refusing certificate deletion")
	}
	if err := m.certificates.RemoveTrust(state.CAThumbprint); err != nil {
		return "", err
	}
	if err := m.state.Delete(); err != nil {
		return "", err
	}
	return "gh-gateway local mode uninstalled.\n", nil
}
