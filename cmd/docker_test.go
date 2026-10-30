// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func findProjectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate project root containing go.mod")
		}
		dir = parent
	}
}

func TestDockerfileStructure(t *testing.T) {
	rootDir := findProjectRoot(t)
	dockerfilePath := filepath.Join(rootDir, "Dockerfile")

	content, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("failed to read Dockerfile: %v", err)
	}
	text := string(content)

	checks := []struct {
		name     string
		contains string
	}{
		{"builder image", "FROM golang:1.24-alpine AS builder"},
		{"runtime image", "FROM alpine:3.21"},
		{"runtime dependencies", "apk add --no-cache ca-certificates curl"},
		{"copy payloads", "COPY payloads/ /nomore403/payloads/"},
		{"payloads symlink", "RUN ln -s /nomore403/payloads /payloads"},
		{"working directory", "WORKDIR /nomore403"},
		{"entrypoint", `ENTRYPOINT ["nomore403"]`},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if !strings.Contains(text, check.contains) {
				t.Errorf("Dockerfile missing expected directive %q", check.contains)
			}
		})
	}
}

func TestDockerignoreRules(t *testing.T) {
	rootDir := findProjectRoot(t)
	dockerignorePath := filepath.Join(rootDir, ".dockerignore")

	content, err := os.ReadFile(dockerignorePath)
	if err != nil {
		t.Fatalf("failed to read .dockerignore: %v", err)
	}
	text := string(content)

	expectedEntries := []string{
		".git",
		".github",
		".gitignore",
		"dist/",
		".DS_Store",
		"._*",
	}

	for _, entry := range expectedEntries {
		t.Run(entry, func(t *testing.T) {
			if !strings.Contains(text, entry) {
				t.Errorf(".dockerignore missing expected entry %q", entry)
			}
		})
	}
}

func TestDockerWorkflowConfig(t *testing.T) {
	rootDir := findProjectRoot(t)
	workflowPath := filepath.Join(rootDir, ".github", "workflows", "docker.yaml")

	content, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("failed to read docker.yaml workflow: %v", err)
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("failed to parse docker.yaml as YAML: %v", err)
	}

	text := string(content)
	checks := []struct {
		name     string
		expected string
	}{
		{"workflow name", "name: Docker"},
		{"push main trigger", "- main"},
		{"push tags trigger", "- 'v*'"},
		{"pull request trigger", "pull_request:"},
		{"workflow dispatch trigger", "workflow_dispatch:"},
		{"packages write permission", "packages: write"},
		{"ghcr registry", "registry: ghcr.io"},
		{"multi-platform support", "platforms: linux/amd64,linux/arm64"},
		{"docker metadata action", "uses: docker/metadata-action@v5"},
		{"docker build-push action", "uses: docker/build-push-action@v6"},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if !strings.Contains(text, check.expected) {
				t.Errorf("docker.yaml missing expected configuration %q", check.expected)
			}
		})
	}
}

func isDockerAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "info")
	return cmd.Run() == nil
}

func TestDockerBuildAndExecution(t *testing.T) {
	if !isDockerAvailable() {
		t.Skip("Docker daemon is not available; skipping live container execution test")
	}

	rootDir := findProjectRoot(t)
	imageTag := "nomore403-test:unit"

	buildCmd := exec.Command("docker", "build",
		"--build-arg", "VERSION=test-v1.0.0",
		"--build-arg", "BUILD_DATE=2026-10-17",
		"-t", imageTag, rootDir)
	buildOut, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker build failed: %v\nOutput: %s", err, string(buildOut))
	}
	defer func() {
		_ = exec.Command("docker", "rmi", "-f", imageTag).Run()
	}()

	t.Run("HelpFlag", func(t *testing.T) {
		cmd := exec.Command("docker", "run", "--rm", imageTag, "--help")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("docker run --help failed: %v\nOutput: %s", err, string(out))
		}
		if !strings.Contains(string(out), "Command line application that automates different ways to bypass 40X codes.") {
			t.Errorf("help output does not contain expected banner; got: %s", string(out))
		}
	})

	t.Run("VersionFlag", func(t *testing.T) {
		cmd := exec.Command("docker", "run", "--rm", imageTag, "--version")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("docker run --version failed: %v\nOutput: %s", err, string(out))
		}
		expectedVersion := "nomore403 version test-v1.0.0 (built 2026-10-17)"
		if !strings.Contains(string(out), expectedVersion) {
			t.Errorf("version output got %q; want containing %q", string(out), expectedVersion)
		}
	})

	t.Run("PayloadsDirectoryPresence", func(t *testing.T) {
		cmd := exec.Command("docker", "run", "--rm", "--entrypoint", "sh", imageTag, "-c", "ls /nomore403/payloads")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("docker run ls payloads failed: %v\nOutput: %s", err, string(out))
		}
		output := string(out)
		expectedPayloads := []string{
			"endpaths",
			"headers",
			"httpmethods",
			"ips",
			"midpaths",
			"simpleheaders",
			"useragents",
		}
		for _, p := range expectedPayloads {
			if !strings.Contains(output, p) {
				t.Errorf("missing expected payload file %q in /nomore403/payloads", p)
			}
		}
	})

	t.Run("PayloadsSymlinkPresence", func(t *testing.T) {
		cmd := exec.Command("docker", "run", "--rm", "--entrypoint", "sh", imageTag, "-c", "readlink /payloads")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("docker run readlink /payloads failed: %v\nOutput: %s", err, string(out))
		}
		if strings.TrimSpace(string(out)) != "/nomore403/payloads" {
			t.Errorf("/payloads symlink target got %q; want /nomore403/payloads", strings.TrimSpace(string(out)))
		}
	})

	t.Run("StdinTargetsProcessing", func(t *testing.T) {
		input := bytes.NewBufferString("http://127.0.0.1:1\n")
		runCmd := exec.Command("docker", "run", "--rm", "-i", imageTag, "-k", "verb-tampering", "--max-goroutines", "1", "--timeout", "1")
		runCmd.Stdin = input
		out, _ := runCmd.CombinedOutput()
		outputText := string(out)
		if strings.Contains(outputText, "panic") {
			t.Fatalf("container crashed with panic: %s", outputText)
		}
	})
}
