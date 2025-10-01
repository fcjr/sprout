package nix

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

//go:embed image.nix.tmpl
var imageNixTemplate string

func (n *Nix) GenerateImage(sproutFile SproutFile) (string, error) {
	tmpl, err := template.New("image").Parse(imageNixTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, sproutFile)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (n *Nix) BuildSproutBinary() (string, error) {
	fmt.Printf("      \033[36mBuilding Sprout binary for ARM64...\033[0m\n")

	tempDir, err := os.MkdirTemp("", "sprout-binary-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	binaryPath := filepath.Join(tempDir, "sprout")
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/sprout/main.go")
	cmd.Dir = "/Users/fcjr/git/sprout"
	cmd.Env = append(os.Environ(),
		"GOOS=linux",
		"GOARCH=arm64",
		"CGO_ENABLED=0",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("failed to build Sprout binary: %w\nOutput: %s", err, output)
	}

	fmt.Printf("      \033[32m✓ Sprout binary built for ARM64\033[0m\n")
	return binaryPath, nil
}

func (n *Nix) Build(filename string, sproutFile *SproutFile) (string, error) {
	if os.Getenv("SPROUT_DISABLE_LOCAL_NIX") != "" {
		fmt.Printf("      \033[36mLocal Nix disabled via SPROUT_DISABLE_LOCAL_NIX, using Docker build...\033[0m\n")
		return n.buildWithDocker(filename, sproutFile)
	}

	if nixPath, err := exec.LookPath("nix-build"); err == nil {
		fmt.Printf("      \033[36mUsing local Nix installation for faster builds...\033[0m\n")
		return n.buildLocal(nixPath, filename, sproutFile)
	}

	fmt.Printf("      \033[36mNix not found locally, using Docker build...\033[0m\n")
	return n.buildWithDocker(filename, sproutFile)
}
