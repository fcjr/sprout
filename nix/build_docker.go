package nix

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"

	configpkg "github.com/fcjr/sprout/internal/config"
)

func (n *Nix) buildWithDocker(filename string, sproutFile *SproutFile) (string, error) {
	tempDir, nixFileInTemp, err := n.prepareDockerBuildDir(filename, sproutFile)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempDir)

	if err := n.copyDockerImages(sproutFile, tempDir, nixFileInTemp); err != nil {
		return "", err
	}

	n.printDockerBuildInfo()

	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "", fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer cli.Close()

	containerConfig, hostConfig, err := n.createDockerConfigs(tempDir)
	if err != nil {
		return "", err
	}

	nixStorePath, err := n.runDockerBuild(ctx, cli, containerConfig, hostConfig)
	if err != nil {
		return "", err
	}

	fmt.Printf("      \033[32mDocker build completed: %s\033[0m\n", nixStorePath)
	return nixStorePath, nil
}

func (n *Nix) prepareDockerBuildDir(filename string, sproutFile *SproutFile) (string, string, error) {
	absNixFile, err := filepath.Abs(filename)
	if err != nil {
		return "", "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("failed to get home directory: %w", err)
	}
	tempDir, err := os.MkdirTemp(homeDir, "sprout-docker-*")
	if err != nil {
		return "", "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	nixFileInTemp := filepath.Join(tempDir, "image.nix")
	if err := n.copyFile(absNixFile, nixFileInTemp); err != nil {
		os.RemoveAll(tempDir)
		return "", "", fmt.Errorf("failed to copy nix file: %w", err)
	}

	return tempDir, nixFileInTemp, nil
}

func (n *Nix) copyDockerImages(sproutFile *SproutFile, tempDir, nixFileInTemp string) error {
	if !sproutFile.DockerCompose.Enabled {
		return nil
	}

	for _, img := range sproutFile.DockerCompose.Images {
		if _, err := os.Stat(img.TarPath); err == nil {
			tarFileName := filepath.Base(img.TarPath)
			destPath := filepath.Join(tempDir, tarFileName)
			if err := n.copyFile(img.TarPath, destPath); err != nil {
				return fmt.Errorf("failed to copy docker image %s: %w", img.TarPath, err)
			}
			containerTarPath := filepath.Join("/workspace", tarFileName)
			if err := n.updateTarPathInNixFile(nixFileInTemp, img.TarPath, containerTarPath); err != nil {
				return fmt.Errorf("failed to update tar path in nix file: %w", err)
			}
		}
	}
	return nil
}

func (n *Nix) printDockerBuildInfo() {
	fmt.Printf("      \033[36mThis may take 2-8 minutes (optimized with parallel builds)...\033[0m\n")
	fmt.Printf("      \033[36mBuilding with Docker Linux container (4GB RAM, multi-core)...\033[0m\n")
	fmt.Printf("      \033[36mUsing persistent Nix store cache for faster subsequent builds...\033[0m\n")
}

func (n *Nix) createDockerConfigs(tempDir string) (*container.Config, *container.HostConfig, error) {
	containerConfig := &container.Config{
		Image:      "nixos/nix:latest",
		Cmd:        []string{"nix-build", "--cores", "0", "--max-jobs", "auto", "--no-link", "/workspace/image.nix"},
		WorkingDir: "/workspace",
		Env: []string{
			"PATH=/root/.nix-profile/bin:/nix/var/nix/profiles/default/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			"NIX_PATH=nixpkgs=/root/.nix-defexpr/channels/nixpkgs",
			"NIX_BUILD_CORES=0",
			"NIX_CONFIG=cores = 0\nmax-jobs = auto\nsubstituters = https://cache.nixos.org https://cache.nixos.org/\ntrusted-public-keys = cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY=\nfilter-syscalls = false",
		},
	}

	configFolder, err := configpkg.Folder()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get config folder: %w", err)
	}
	nixCacheDir := filepath.Join(configFolder, "cache")
	if err := os.MkdirAll(nixCacheDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("failed to create nix cache directory: %w", err)
	}

	nixStoreDir := filepath.Join(configFolder, "nix-store")
	if err := os.MkdirAll(nixStoreDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("failed to create nix store directory: %w", err)
	}

	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: tempDir,
				Target: "/workspace",
			},
			{
				Type:   mount.TypeBind,
				Source: nixCacheDir,
				Target: "/root/.cache/nix",
			},
			{
				Type:   mount.TypeBind,
				Source: nixStoreDir,
				Target: "/nix",
			},
		},
		AutoRemove: true,
		Resources: container.Resources{
			CPUShares: 1024,
			Memory:    4294967296,
		},
		Privileged: true,
	}

	return containerConfig, hostConfig, nil
}

func (n *Nix) runDockerBuild(ctx context.Context, cli *client.Client, containerConfig *container.Config, hostConfig *container.HostConfig) (string, error) {
	resp, err := cli.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "")
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	return n.streamDockerLogs(ctx, cli, resp.ID)
}

func (n *Nix) streamDockerLogs(ctx context.Context, cli *client.Client, containerID string) (string, error) {
	logs, err := cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return "", fmt.Errorf("failed to get container logs: %w", err)
	}

	var buildOutput strings.Builder
	go n.processDockerLogs(logs, &buildOutput)

	statusCh, errCh := cli.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		logs.Close()
		if err != nil {
			return "", fmt.Errorf("error waiting for container: %w", err)
		}
	case status := <-statusCh:
		logs.Close()
		if status.StatusCode != 0 {
			return "", fmt.Errorf("container exited with code %d", status.StatusCode)
		}
	}

	return n.extractNixStorePath(buildOutput.String())
}

func (n *Nix) processDockerLogs(logs io.ReadCloser, buildOutput *strings.Builder) {
	defer finishStreamingDisplay()

	for {
		header := make([]byte, 8)
		n, err := io.ReadFull(logs, header)
		if err != nil || n != 8 {
			break
		}

		payloadSize := int(header[4])<<24 | int(header[5])<<16 | int(header[6])<<8 | int(header[7])

		if payloadSize <= 0 {
			continue
		}

		payload := make([]byte, payloadSize)
		n, err = io.ReadFull(logs, payload)
		if err != nil || n != payloadSize {
			break
		}

		scanner := bufio.NewScanner(bytes.NewReader(payload))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				buildOutput.WriteString(line + "\n")
				displayStreamingLine(line, "\033[36m")
			}
		}
	}
}

func (n *Nix) extractNixStorePath(output string) (string, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var nixStorePath string
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "/nix/store/") {
			nixStorePath = line
			break
		}
	}

	if nixStorePath == "" {
		return "", fmt.Errorf("could not find Nix store path in build output")
	}

	return nixStorePath, nil
}
