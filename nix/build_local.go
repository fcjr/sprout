package nix

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (n *Nix) buildLocal(nixPath, filename string, sproutFile *SproutFile) (string, error) {
	absNixFile, err := filepath.Abs(filename)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	fmt.Printf("      \033[36mBuilding NixOS image locally...\033[0m\n")

	cmd := exec.Command(nixPath, "--cores", "0", "--max-jobs", "auto", "--no-link", absNixFile)
	cmd.Env = append(os.Environ(),
		"NIX_BUILD_CORES=0",
		"NIX_CONFIG=cores = 0\nmax-jobs = auto\nsubstituters = https://cache.nixos.org\ntrusted-public-keys = cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY=",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start nix-build: %w", err)
	}

	var buildResult string
	resultChan := make(chan string, 1)

	go func() {
		scanner := bufio.NewScanner(stdout)
		var lastLine string
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				displayStreamingLine(line, "\033[36m")
				lastLine = line
			}
		}
		resultChan <- lastLine
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				displayStreamingLine(line, "\033[33m")
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("failed to build Nix configuration: %w", err)
	}

	buildResult = <-resultChan
	finishStreamingDisplay()

	fmt.Printf("      \033[32mLocal build completed: %s\033[0m\n", buildResult)

	actualImagePath, err := FindImageFile(buildResult)
	if err != nil {
		return "", fmt.Errorf("failed to find image file in build result: %w", err)
	}

	return actualImagePath, nil
}
