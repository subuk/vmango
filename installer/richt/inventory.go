package richt

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"subuk/vmango/installer/recipes"
)

// RecipeRevision is updated only when reviewed recipe sources are published.
// Every reference also pins the exact source bytes independently of the URL.
const RecipeRevision = "6b6b25708b018f6297cd0ca591e0eaf967dc35d2"

func recipeFor(osID, version, arch string) (string, error) {
	if arch != "x86_64" && arch != "aarch64" {
		return "", errors.New("installer recipes support x86_64 and aarch64 targets")
	}
	switch osID {
	case "ubuntu":
		if version == "24.04" {
			if arch == "aarch64" {
				return "ubuntu-24.04-arm64.yaml", nil
			}
			return "ubuntu-24.04-amd64.yaml", nil
		}
	case "almalinux", "rhel", "rocky", "centos":
		if strings.Split(version, ".")[0] == "10" {
			return "rhel-10.yaml", nil
		}
	}
	return "", fmt.Errorf("no reviewed installer recipe for %s %s %s (supported: Ubuntu 24.04 and RHEL-compatible 10)", osID, version, arch)
}

func inventory(targets []Target) (map[string]any, map[string]string, error) {
	return inventoryAtRevision(targets, RecipeRevision)
}

func inventoryAtRevision(targets []Target, revision string) (map[string]any, map[string]string, error) {
	if len(revision) != 40 {
		return nil, nil, errors.New("installer recipe sources have not been published and pinned")
	}
	services := map[string]any{}
	tags := map[string]any{}
	hosts := []any{}
	pins := map[string]string{}
	seen := map[string]bool{}
	for _, t := range targets {
		if err := t.Host.Validate(); err != nil {
			return nil, nil, err
		}
		if seen[t.ID] {
			return nil, nil, errors.New("duplicate installer host identifier")
		}
		seen[t.ID] = true
		file, err := recipeFor(t.OSID, t.OSVersion, t.Architecture)
		if err != nil {
			return nil, nil, err
		}
		body, err := recipes.Sources.ReadFile(file)
		if err != nil {
			return nil, nil, err
		}
		recipe := strings.ReplaceAll(strings.TrimSuffix(file, ".yaml"), ".", "-")
		sum := digest(body)
		services[recipe] = map[string]any{"url": "https://raw.githubusercontent.com/subuk/vmango/" + revision + "/installer/recipes/" + file, "sha256": sum}
		pins[recipe] = sum
		assignments := map[string]any{recipe: map[string]any{}}
		configs := []any{}
		if t.RuntimeUser != "" && t.RuntimeUser != "root" {
			if !username.MatchString(t.RuntimeUser) || !username.MatchString(t.RuntimeGroup) {
				return nil, nil, errors.New("runtime account and existing primary group must be valid local names")
			}
			configs = append(configs, map[string]any{"type": "user", "user": t.RuntimeUser, "group": t.RuntimeGroup, "supplementary_groups": []string{"libvirt"}, "append_groups": true})
		}
		paths := map[string]bool{}
		for _, p := range t.PoolPaths {
			if !filepath.IsAbs(p) || filepath.Clean(p) != p || p == "/" || strings.ContainsAny(p, "\x00\r\n") {
				return nil, nil, errors.New("storage directories must be clean absolute non-root paths")
			}
			if !paths[p] {
				configs = append(configs, map[string]any{"type": "directory", "path": p})
				paths[p] = true
			}
		}
		if len(configs) > 0 {
			name := "host-" + t.ID
			services[name] = map[string]any{"order": 20, "configs": configs}
			assignments[name] = map[string]any{}
		}
		tag := "host-" + t.ID
		tags[tag] = map[string]any{"services": assignments}
		hosts = append(hosts, map[string]any{"name": t.ID, "tags": []string{tag}, "transport": transport(t.Host)})
	}
	return map[string]any{"services": services, "tags": tags, "hosts": hosts}, pins, nil
}
