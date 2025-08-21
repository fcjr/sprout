package nix

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"gopkg.in/yaml.v3"
)

//go:embed image.nix.tmpl
var imageNixTemplate string

type Nix struct{}

type NetworkConfig struct {
	PSK string `yaml:"psk"`
}

type WirelessConfig struct {
	Enabled  bool                     `yaml:"enabled"`
	Networks map[string]NetworkConfig `yaml:"networks"`
}

type OutputConfig struct {
	Path string `yaml:"path"`
}

type DockerImage struct {
	Name     string // Original image name from compose file
	LocalTag string // Local tag to use in the SD image
	TarPath  string // Path to the saved image tar file
}

type DockerComposeConfig struct {
	Enabled         bool          `yaml:"enabled"`
	Path            string        `yaml:"path"`
	Content         string        // Will hold the actual docker-compose.yaml content
	ModifiedContent string        // Modified compose content with local image references
	Images          []DockerImage // Docker images to embed
}

type ImageConfig struct {
	SSHKeys       []string            `yaml:"ssh_keys"`
	Wireless      WirelessConfig      `yaml:"wireless"`
	Output        OutputConfig        `yaml:"output"`
	DockerCompose DockerComposeConfig `yaml:"docker_compose"`
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

	// Load docker-compose.yaml content if enabled and path is specified
	if config.DockerCompose.Enabled && config.DockerCompose.Path != "" {
		// Make path relative to the sprout.yaml file location
		configDir := filepath.Dir(filename)
		dockerComposePath := config.DockerCompose.Path
		if !filepath.IsAbs(dockerComposePath) {
			dockerComposePath = filepath.Join(configDir, dockerComposePath)
		}

		dockerComposeData, err := os.ReadFile(dockerComposePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read docker-compose file at %s: %w", dockerComposePath, err)
		}

		config.DockerCompose.Content = string(dockerComposeData)

		// Parse the docker-compose file and extract Docker images
		err = n.processDockerComposeImages(&config.DockerCompose, dockerComposePath)
		if err != nil {
			return nil, fmt.Errorf("failed to process docker-compose images: %w", err)
		}
	}

	return &config, nil
}

func (n *Nix) processDockerComposeImages(dockerConfig *DockerComposeConfig, composePath string) error {
	// Parse docker-compose file using compose-go CLI
	projectName := "sprout-embedded"
	ctx := context.Background()

	options, err := cli.NewProjectOptions(
		[]string{composePath},
		cli.WithOsEnv,
		cli.WithDotEnv,
		cli.WithName(projectName),
	)
	if err != nil {
		return fmt.Errorf("failed to create project options: %w", err)
	}

	project, err := options.LoadProject(ctx)
	if err != nil {
		return fmt.Errorf("failed to parse docker-compose file: %w", err)
	}

	// Extract images from services
	var images []DockerImage
	imageMap := make(map[string]bool) // To avoid duplicates

	for _, service := range project.Services {
		var imageName string

		if service.Image != "" {
			// Service uses a pre-built image
			imageName = service.Image
		} else if service.Build != nil {
			// Service builds from Dockerfile - we'll build it and give it a local name
			imageName = fmt.Sprintf("%s:latest", service.Name)
		}

		if imageName != "" && !imageMap[imageName] {
			// Create a safe local tag and filename
			safeImageName := strings.ReplaceAll(imageName, "/", "_")
			safeImageName = strings.ReplaceAll(safeImageName, ":", "_")
			safeImageName = strings.ReplaceAll(safeImageName, "-", "_")

			localTag := fmt.Sprintf("embedded/%s", safeImageName)
			tarFileName := fmt.Sprintf("%s.tar", safeImageName)

			images = append(images, DockerImage{
				Name:     imageName,
				LocalTag: localTag,
				TarPath:  fmt.Sprintf("/tmp/%s", tarFileName),
			})
			imageMap[imageName] = true
		}
	}

	dockerConfig.Images = images

	// Create modified compose content with local image references
	err = n.createModifiedComposeContent(dockerConfig, project)
	if err != nil {
		return fmt.Errorf("failed to create modified compose content: %w", err)
	}

	// Build and save Docker images
	err = n.buildAndSaveDockerImages(dockerConfig, filepath.Dir(composePath))
	if err != nil {
		return fmt.Errorf("failed to build and save docker images: %w", err)
	}

	return nil
}

func (n *Nix) createModifiedComposeContent(dockerConfig *DockerComposeConfig, project *types.Project) error {
	// Create a map of original image names to local tags
	imageMapping := make(map[string]string)
	for _, img := range dockerConfig.Images {
		imageMapping[img.Name] = img.LocalTag
	}

	// Modify the project to use local image references
	for serviceName, service := range project.Services {
		if service.Image != "" && imageMapping[service.Image] != "" {
			service.Image = imageMapping[service.Image]
		} else if service.Build != nil {
			// For services with build context, replace with local tag
			localTag := fmt.Sprintf("embedded/%s_latest", serviceName)
			service.Image = localTag
			service.Build = nil // Remove build context since image is pre-built
		}
		project.Services[serviceName] = service
	}

	// Convert back to YAML
	modifiedContent, err := yaml.Marshal(project)
	if err != nil {
		return fmt.Errorf("failed to marshal modified compose content: %w", err)
	}

	dockerConfig.ModifiedContent = string(modifiedContent)
	return nil
}

func (n *Nix) buildAndSaveDockerImages(dockerConfig *DockerComposeConfig, workingDir string) error {
	fmt.Println("Building and saving Docker images...")

	for i, img := range dockerConfig.Images {
		fmt.Printf("Processing image: %s\n", img.Name)

		// Check if this is a service that needs to be built
		if strings.HasSuffix(img.Name, ":latest") && !strings.Contains(img.Name, "/") {
			// This is likely a local build - run docker-compose build for this service
			serviceName := strings.TrimSuffix(img.Name, ":latest")
			cmd := exec.Command("docker-compose", "build", serviceName)
			cmd.Dir = workingDir
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to build service %s: %w", serviceName, err)
			}
		} else {
			// Pull the image if it's not local
			cmd := exec.Command("docker", "pull", img.Name)
			if err := cmd.Run(); err != nil {
				fmt.Printf("Warning: failed to pull image %s, assuming it exists locally: %v\n", img.Name, err)
			}
		}

		// Tag the image with local tag
		cmd := exec.Command("docker", "tag", img.Name, img.LocalTag)
		var tagStderr bytes.Buffer
		cmd.Stderr = &tagStderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to tag image %s as %s: %w (stderr: %s)", img.Name, img.LocalTag, err, tagStderr.String())
		}

		// Save the image to tar file
		cmd = exec.Command("docker", "save", "-o", img.TarPath, img.LocalTag)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to save image %s to %s: %w (stderr: %s)", img.LocalTag, img.TarPath, err, stderr.String())
		}

		fmt.Printf("Saved image %s to %s\n", img.LocalTag, img.TarPath)
		dockerConfig.Images[i] = img
	}

	return nil
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
