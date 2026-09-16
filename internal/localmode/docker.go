package localmode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type containerSpec struct {
	Name, InstanceID, Host, UpstreamIP, Image, LeafCert, LeafKey, CertVolume string
	SSHPort                                                                  int
	SSHProxy                                                                 bool
}

type containerInfo struct {
	Exists, Running, HTTPSPublished, SSHPublished, CertsReadOnly, VolumeExists bool
	ID                                                                         string
}

type dockerRuntime interface {
	Check(context.Context) error
	Start(context.Context, containerSpec) (string, error)
	Inspect(context.Context, string, int, bool) (containerInfo, error)
	Remove(context.Context, string) error
}

type commandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}
type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type dockerCLI struct{ runner commandRunner }

func (d dockerCLI) Check(ctx context.Context) error {
	output, err := d.runner.Run(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	if err != nil {
		return fmt.Errorf("Docker is not reachable: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func dockerRunArguments(spec containerSpec) []string {
	args := []string{"create", "--name", spec.Name, "--restart", "no", "--read-only", "--label", "io.gh-gateway.managed=true", "--label", "io.gh-gateway.instance=" + spec.InstanceID, "--label", "io.gh-gateway.host=" + spec.Host, "-p", "127.0.0.1:443:443", "-v", spec.CertVolume + ":/certs:ro", "-e", "GITEA_BASE_URL=https://" + spec.Host, "-e", "GATEWAY_ADDR=:443", "-e", "GATEWAY_TRANSPARENT_HOST=" + spec.Host, "-e", "GATEWAY_UPSTREAM_IP=" + spec.UpstreamIP, "-e", "GATEWAY_TLS_CERT=/certs/leaf.crt", "-e", "GATEWAY_TLS_KEY=/certs/leaf.key"}
	if spec.SSHProxy {
		port := strconv.Itoa(spec.SSHPort)
		args = append(args, "-p", "127.0.0.1:"+port+":"+port, "-e", "GATEWAY_SSH_PROXY=1", "-e", "GATEWAY_SSH_PORT="+port)
	}
	return append(args, spec.Image, "serve")
}

func (d dockerCLI) Start(ctx context.Context, spec containerSpec) (string, error) {
	loaderName := spec.Name + "-cert-loader"
	cleanup := func() {
		_, _ = d.runner.Run(context.Background(), "docker", "rm", "-f", loaderName)
		_, _ = d.runner.Run(context.Background(), "docker", "rm", "-f", spec.Name)
		_, _ = d.runner.Run(context.Background(), "docker", "volume", "rm", "-f", spec.CertVolume)
	}
	output, err := d.runner.Run(ctx, "docker", "volume", "create", "--label", "io.gh-gateway.managed=true", "--label", "io.gh-gateway.instance="+spec.InstanceID, spec.CertVolume)
	if err != nil {
		return "", fmt.Errorf("create Docker certificate volume: %w: %s", err, strings.TrimSpace(string(output)))
	}
	output, err = d.runner.Run(ctx, "docker", "create", "--name", loaderName, "--label", "io.gh-gateway.managed=true", "--entrypoint", "/bin/sh", "-v", spec.CertVolume+":/certs", spec.Image, "-c", "chmod 0444 /certs/leaf.crt && chmod 0400 /certs/leaf.key")
	if err != nil {
		cleanup()
		return "", fmt.Errorf("create Docker certificate loader: %w: %s", err, strings.TrimSpace(string(output)))
	}
	for _, certificate := range []struct{ source, target string }{{spec.LeafCert, "/certs/leaf.crt"}, {spec.LeafKey, "/certs/leaf.key"}} {
		output, err = d.runner.Run(ctx, "docker", "cp", certificate.source, loaderName+":"+certificate.target)
		if err != nil {
			cleanup()
			return "", fmt.Errorf("copy TLS material into Docker container: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	output, err = d.runner.Run(ctx, "docker", "start", "--attach", loaderName)
	if err != nil {
		cleanup()
		return "", fmt.Errorf("secure Docker TLS material: %w: %s", err, strings.TrimSpace(string(output)))
	}
	output, err = d.runner.Run(ctx, "docker", "rm", loaderName)
	if err != nil {
		cleanup()
		return "", fmt.Errorf("remove Docker certificate loader: %w: %s", err, strings.TrimSpace(string(output)))
	}
	output, err = d.runner.Run(ctx, "docker", dockerRunArguments(spec)...)
	if err != nil {
		cleanup()
		return "", fmt.Errorf("create Docker container: %w: %s", err, strings.TrimSpace(string(output)))
	}
	containerID := strings.TrimSpace(string(output))
	output, err = d.runner.Run(ctx, "docker", "start", containerID)
	if err != nil {
		cleanup()
		return "", fmt.Errorf("start Docker container: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return containerID, nil
}

func (d dockerCLI) Inspect(ctx context.Context, name string, sshPort int, sshProxy bool) (containerInfo, error) {
	info := containerInfo{}
	volumeOutput, volumeErr := d.runner.Run(ctx, "docker", "volume", "inspect", certVolumeName(name))
	if volumeErr == nil {
		info.VolumeExists = true
	} else if !strings.Contains(strings.ToLower(string(volumeOutput)), "no such volume") {
		return info, fmt.Errorf("inspect Docker certificate volume: %w: %s", volumeErr, strings.TrimSpace(string(volumeOutput)))
	}
	output, err := d.runner.Run(ctx, "docker", "inspect", name)
	if err != nil {
		if strings.Contains(strings.ToLower(string(output)), "no such object") {
			return info, nil
		}
		return containerInfo{}, fmt.Errorf("inspect Docker container: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var values []struct {
		ID    string `json:"Id"`
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
		NetworkSettings struct {
			Ports map[string][]struct{ HostIP, HostPort string } `json:"Ports"`
		} `json:"NetworkSettings"`
		Mounts []struct {
			Name, Destination string
			RW                bool
		} `json:"Mounts"`
	}
	if err := json.Unmarshal(output, &values); err != nil || len(values) != 1 {
		return containerInfo{}, errors.New("decode Docker inspect response")
	}
	info.Exists, info.Running, info.ID = true, values[0].State.Running, values[0].ID
	info.HTTPSPublished = hasPublishedPort(values[0].NetworkSettings.Ports["443/tcp"], 443)
	if sshProxy {
		info.SSHPublished = hasPublishedPort(values[0].NetworkSettings.Ports[strconv.Itoa(sshPort)+"/tcp"], sshPort)
	} else {
		info.SSHPublished = true
	}
	for _, mount := range values[0].Mounts {
		if mount.Name == certVolumeName(name) && mount.Destination == "/certs" && !mount.RW {
			info.CertsReadOnly = true
		}
	}
	return info, nil
}

func hasPublishedPort(bindings []struct{ HostIP, HostPort string }, port int) bool {
	want := strconv.Itoa(port)
	for _, binding := range bindings {
		if binding.HostIP == "127.0.0.1" && binding.HostPort == want {
			return true
		}
	}
	return false
}

func (d dockerCLI) Remove(ctx context.Context, name string) error {
	if name == "" {
		return nil
	}
	output, err := d.runner.Run(ctx, "docker", "rm", "-f", name)
	if err != nil && !strings.Contains(strings.ToLower(string(output)), "no such container") {
		return fmt.Errorf("remove Docker container: %w: %s", err, strings.TrimSpace(string(output)))
	}
	_, _ = d.runner.Run(ctx, "docker", "rm", "-f", name+"-cert-loader")
	output, err = d.runner.Run(ctx, "docker", "volume", "rm", "-f", certVolumeName(name))
	if err != nil && !strings.Contains(strings.ToLower(string(output)), "no such volume") {
		return fmt.Errorf("remove Docker certificate volume: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func certVolumeName(container string) string { return container + "-certs" }
