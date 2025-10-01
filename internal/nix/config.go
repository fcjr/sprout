package nix

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadConfig loads the sprout.yaml config and processes Docker images.
// Use this when building images (seed command).
func (n *Nix) LoadConfig(filename string) (*SproutFile, error) {
	return n.loadConfig(filename, true)
}

// LoadConfigOnly loads the sprout.yaml config without processing Docker images.
// Use this when you only need to read config values (like burn command reading output path).
func (n *Nix) LoadConfigOnly(filename string) (*SproutFile, error) {
	return n.loadConfig(filename, false)
}

func (n *Nix) loadConfig(filename string, processDocker bool) (*SproutFile, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var sproutFile SproutFile
	err = yaml.Unmarshal(data, &sproutFile)
	if err != nil {
		return nil, err
	}

	shouldProcess := processDocker && sproutFile.DockerCompose.Enabled && sproutFile.DockerCompose.Path != ""
	if !shouldProcess {
		return &sproutFile, nil
	}

	configDir := filepath.Dir(filename)
	dockerComposePath := sproutFile.DockerCompose.Path
	if !filepath.IsAbs(dockerComposePath) {
		dockerComposePath = filepath.Join(configDir, dockerComposePath)
	}

	dockerComposeData, err := os.ReadFile(dockerComposePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read docker-compose file at %s: %w", dockerComposePath, err)
	}

	sproutFile.DockerCompose.Content = string(dockerComposeData)

	err = n.processDockerComposeImages(&sproutFile.DockerCompose, dockerComposePath)
	if err != nil {
		return nil, fmt.Errorf("failed to process docker-compose images: %w", err)
	}

	return &sproutFile, nil
}
