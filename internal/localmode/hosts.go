package localmode

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type hostsInspection struct{ Own, Conflict bool }

type hostsManager interface {
	Inspect(host, instanceID string) (hostsInspection, error)
	Install(host, instanceID string) error
	Remove(instanceID string) error
}

type fileHostsManager struct{ path string }

func (m fileHostsManager) Inspect(host, instanceID string) (hostsInspection, error) {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return hostsInspection{}, fmt.Errorf("read hosts file: %w", err)
	}
	return inspectHosts(string(data), host, instanceID), nil
}

func inspectHosts(contents, host, instanceID string) hostsInspection {
	result := hostsInspection{}
	insideOwn := false
	for _, raw := range strings.Split(contents, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == markerBegin(instanceID) {
			insideOwn = true
			continue
		}
		if line == markerEnd(instanceID) {
			insideOwn = false
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if index := strings.IndexByte(line, '#'); index >= 0 {
			line = line[:index]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		for _, candidate := range fields[1:] {
			if strings.EqualFold(strings.TrimSuffix(candidate, "."), strings.TrimSuffix(host, ".")) {
				if insideOwn {
					result.Own = true
				} else {
					result.Conflict = true
				}
			}
		}
	}
	return result
}

func (m fileHostsManager) Install(host, instanceID string) error {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return fmt.Errorf("read hosts file: %w", err)
	}
	inspection := inspectHosts(string(data), host, instanceID)
	if inspection.Conflict {
		return fmt.Errorf("host %s is already managed by another hosts entry", host)
	}
	if inspection.Own {
		return nil
	}
	newline := "\r\n"
	if strings.Contains(string(data), "\n") && !strings.Contains(string(data), "\r\n") {
		newline = "\n"
	}
	contents := string(data)
	if contents != "" && !strings.HasSuffix(contents, "\n") {
		contents += newline
	}
	contents += markerBegin(instanceID) + newline + "127.0.0.1 " + host + newline + markerEnd(instanceID) + newline
	return replaceFile(m.path, []byte(contents))
}

func (m fileHostsManager) Remove(instanceID string) error {
	data, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read hosts file: %w", err)
	}
	lines := strings.SplitAfter(string(data), "\n")
	begin, end := markerBegin(instanceID), markerEnd(instanceID)
	inside, found := false, false
	kept := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r\n"))
		if !inside && line == begin {
			inside, found = true, true
			continue
		}
		if inside {
			if line == end {
				inside = false
			}
			continue
		}
		kept = append(kept, raw)
	}
	if inside {
		return errors.New("gh-gateway hosts marker is incomplete; refusing to rewrite hosts")
	}
	if !found {
		return nil
	}
	return replaceFile(m.path, []byte(strings.Join(kept, "")))
}

func replaceFile(path string, contents []byte) error {
	return replaceFileContents(path, contents)
}

func markerBegin(id string) string { return "# gh-gateway begin " + id }
func markerEnd(id string) string   { return "# gh-gateway end " + id }
