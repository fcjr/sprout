package nix

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

//go:embed image.nix.tmpl
var imageNixTemplate string

type Nix struct{}

type NetworkConfig struct {
	PSK string `yaml:"psk"`
}

type WirelessConfig struct {
	Enabled  bool                      `yaml:"enabled"`
	Networks map[string]NetworkConfig `yaml:"networks"`
}

type OutputConfig struct {
	Path string `yaml:"path"`
}

type ImageConfig struct {
	SSHKeys  []string       `yaml:"ssh_keys"`
	Wireless WirelessConfig `yaml:"wireless"`
	Output   OutputConfig   `yaml:"output"`
}

func (n *Nix) LoadConfigFromYAML(filename string) (*ImageConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config ImageConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func (n *Nix) GenerateImage(config ImageConfig) (string, error) {
	tmpl, err := template.New("image").Parse(imageNixTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, config)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (n *Nix) Build(filename string) (string, error) {
	cmd := exec.Command("nix-build", "--no-link", filename)
	
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("nix-build failed: %v\nstderr: %s", err, stderr.String())
	}
	
	output := strings.TrimSpace(stdout.String())
	if output == "" {
		return "", fmt.Errorf("nix-build produced no output")
	}
	
	// Get the last line of output which should be the path to the built image
	lines := strings.Split(output, "\n")
	imagePath := strings.TrimSpace(lines[len(lines)-1])
	
	return imagePath, nil
}
