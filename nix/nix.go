package nix

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"gopkg.in/yaml.v3"

	configpkg "github.com/fcjr/sprout/internal/config"
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

type SproutFile struct {
	SSHKeys          []string            `yaml:"ssh_keys"`
	Wireless         WirelessConfig      `yaml:"wireless"`
	Output           OutputConfig        `yaml:"output"`
	DockerCompose    DockerComposeConfig `yaml:"docker_compose"`
	Autodiscovery    bool                `yaml:"autodiscovery"`
	SproutBinaryPath string              // Path to the built Sprout binary for embedding
}

func (n *Nix) LoadSproutFileFromYAML(filename string) (*SproutFile, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var sproutFile SproutFile
	err = yaml.Unmarshal(data, &sproutFile)
	if err != nil {
		return nil, err
	}

	// Load docker-compose.yaml content if enabled and path is specified
	if sproutFile.DockerCompose.Enabled && sproutFile.DockerCompose.Path != "" {
		// Make path relative to the sprout.yaml file location
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

		// Parse the docker-compose file and extract Docker images
		err = n.processDockerComposeImages(&sproutFile.DockerCompose, dockerComposePath)
		if err != nil {
			return nil, fmt.Errorf("failed to process docker-compose images: %w", err)
		}
	}

	return &sproutFile, nil
}

// LoadSproutFileFromYAMLLightweight loads the YAML config without processing Docker images
// This is useful for commands that only need basic config info (like burn command)
func (n *Nix) LoadSproutFileFromYAMLLightweight(filename string) (*SproutFile, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var sproutFile SproutFile
	err = yaml.Unmarshal(data, &sproutFile)
	if err != nil {
		return nil, err
	}

	// Skip Docker processing - just return the basic config
	return &sproutFile, nil
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
	fmt.Printf("      \033[36mBuilding and saving Docker images...\033[0m\n")

	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer cli.Close()

	for i, img := range dockerConfig.Images {
		fmt.Printf("      \033[36mProcessing: %s\033[0m\n", img.Name)

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
			// Pull the image if it's not local using Docker client
			// Try ARM64 first, fall back to default platform if that fails
			pullOptions := image.PullOptions{
				Platform: "linux/arm64",
			}
			reader, err := cli.ImagePull(ctx, img.Name, pullOptions)
			if err != nil {
				fmt.Printf("      ARM64 image not available, trying default platform: %s\n", img.Name)
				// Fallback to default platform (usually amd64)
				pullOptions = image.PullOptions{}
				reader, err = cli.ImagePull(ctx, img.Name, pullOptions)
				if err != nil {
					fmt.Printf("Warning: failed to pull image %s, assuming it exists locally: %v\n", img.Name, err)
				} else {
					// Consume the pull response to ensure the pull completes
					io.Copy(io.Discard, reader)
					reader.Close()
				}
			} else {
				fmt.Printf("      Successfully pulled ARM64 image: %s\n", img.Name)
				// Consume the pull response to ensure the pull completes
				io.Copy(io.Discard, reader)
				reader.Close()
			}
		}

		// Tag the image with local tag using Docker client
		err := cli.ImageTag(ctx, img.Name, img.LocalTag)
		if err != nil {
			return fmt.Errorf("failed to tag image %s as %s: %w", img.Name, img.LocalTag, err)
		}

		// Save the image to tar file using Docker client
		reader, err := cli.ImageSave(ctx, []string{img.LocalTag})
		if err != nil {
			return fmt.Errorf("failed to save image %s: %w", img.LocalTag, err)
		}
		defer reader.Close()

		// Create the tar file
		tarFile, err := os.Create(img.TarPath)
		if err != nil {
			return fmt.Errorf("failed to create tar file %s: %w", img.TarPath, err)
		}
		defer tarFile.Close()

		// Copy the image data to the tar file
		_, err = io.Copy(tarFile, reader)
		if err != nil {
			return fmt.Errorf("failed to write image to tar file %s: %w", img.TarPath, err)
		}

		fmt.Printf("      \033[32mSaved: %s\033[0m\n", img.LocalTag)
		dockerConfig.Images[i] = img
	}

	return nil
}

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

// BuildSproutBinary builds the Sprout binary for ARM64 Linux
func (n *Nix) BuildSproutBinary() (string, error) {
	fmt.Printf("      \033[36mBuilding Sprout binary for ARM64...\033[0m\n")

	// Create temporary directory for the binary
	tempDir, err := os.MkdirTemp("", "sprout-binary-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Build the binary for ARM64 Linux
	binaryPath := filepath.Join(tempDir, "sprout")
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/sprout/main.go")
	cmd.Dir = "/Users/fcjr/git/sprout" // Set working directory to the module root
	cmd.Env = append(os.Environ(),
		"GOOS=linux",
		"GOARCH=arm64",
		"CGO_ENABLED=0",
	)

	// Get the build output for debugging
	output, err := cmd.CombinedOutput()
	if err != nil {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("failed to build Sprout binary: %w\nOutput: %s", err, output)
	}

	fmt.Printf("      \033[32m✓ Sprout binary built for ARM64\033[0m\n")
	return binaryPath, nil
}

func (n *Nix) Build(filename string, sproutFile *SproutFile) (string, error) {
	// Check if local Nix builds are disabled via environment variable
	if os.Getenv("SPROUT_DISABLE_LOCAL_NIX") != "" {
		fmt.Printf("      \033[36mLocal Nix disabled via SPROUT_DISABLE_LOCAL_NIX, using Docker build...\033[0m\n")
		return n.buildWithDocker(filename, sproutFile)
	}

	// Check if nix-build is available locally (much faster than Docker)
	if nixPath, err := exec.LookPath("nix-build"); err == nil {
		fmt.Printf("      \033[36mUsing local Nix installation for faster builds...\033[0m\n")
		return n.buildLocal(nixPath, filename, sproutFile)
	}

	fmt.Printf("      \033[36mNix not found locally, using Docker build...\033[0m\n")
	return n.buildWithDocker(filename, sproutFile)
}

func (n *Nix) buildLocal(nixPath, filename string, sproutFile *SproutFile) (string, error) {
	// Get absolute path of the Nix file
	absNixFile, err := filepath.Abs(filename)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	fmt.Printf("      \033[36mBuilding NixOS image locally...\033[0m\n")

	// Run nix-build with optimizations
	cmd := exec.Command(nixPath, "--cores", "0", "--max-jobs", "auto", "--no-link", absNixFile)
	cmd.Env = append(os.Environ(),
		"NIX_BUILD_CORES=0",
		"NIX_CONFIG=cores = 0\nmax-jobs = auto\nsubstituters = https://cache.nixos.org\ntrusted-public-keys = cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY=",
	)

	// Create pipes for streaming output
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

	// Stream output using unified display system
	var buildResult string
	resultChan := make(chan string, 1)

	// Stream stdout with rolling display and capture final result
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

	// Stream stderr with different color
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				displayStreamingLine(line, "\033[33m")
			}
		}
	}()

	// Wait for command completion
	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("failed to build Nix configuration: %w", err)
	}

	buildResult = <-resultChan
	finishStreamingDisplay()

	fmt.Printf("      \033[32mLocal build completed: %s\033[0m\n", buildResult)

	// Find the actual image file inside the build result directory
	actualImagePath, err := FindImageFile(buildResult)
	if err != nil {
		return "", fmt.Errorf("failed to find image file in build result: %w", err)
	}

	return actualImagePath, nil
}

func FindImageFile(basePath string) (string, error) {
	// Check what we're dealing with
	fileInfo, err := os.Stat(basePath)
	if err != nil {
		return "", fmt.Errorf("path does not exist: %s", basePath)
	}

	// If it's actually a file with .img extension, return it
	if !fileInfo.IsDir() && filepath.Ext(basePath) == ".img" {
		return basePath, nil
	}

	// For Docker builds, the image is directly copied as result.img
	if strings.Contains(basePath, "sprout-docker-") {
		// Check if basePath is already the direct path to result.img
		if filepath.Base(basePath) == "result.img" {
			if !fileInfo.IsDir() {
				return basePath, nil
			}
			return "", fmt.Errorf("Docker build completed but image file not found at %s", basePath)
		}
		// Otherwise, look for result.img in the temp directory
		resultImg := filepath.Join(basePath, "result.img")
		if _, err := os.Stat(resultImg); err == nil {
			return resultImg, nil
		}
		return "", fmt.Errorf("Docker build completed but image file not found at %s", resultImg)
	}

	// For all other cases (including local Nix builds), look for sd-image subdirectory
	sdImageDir := filepath.Join(basePath, "sd-image")

	// Check if sd-image directory exists
	if _, err := os.Stat(sdImageDir); os.IsNotExist(err) {
		return "", fmt.Errorf("image file not found: neither %s nor %s/sd-image/*.img exists", basePath, basePath)
	}

	// List files in sd-image directory
	entries, err := os.ReadDir(sdImageDir)
	if err != nil {
		return "", fmt.Errorf("failed to read sd-image directory: %w", err)
	}

	// Find the .img file
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".img" {
			return filepath.Join(sdImageDir, entry.Name()), nil
		}
	}

	return "", fmt.Errorf("no .img file found in sd-image directory")
}

func (n *Nix) buildWithDocker(filename string, sproutFile *SproutFile) (string, error) {
	// Get absolute path of the Nix file
	absNixFile, err := filepath.Abs(filename)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Create a temporary directory for this build - use home directory to ensure it's accessible to Colima
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	tempDir, err := os.MkdirTemp(homeDir, "sprout-docker-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Copy the nix file to temp directory
	nixFileInTemp := filepath.Join(tempDir, "image.nix")
	if err := n.copyFile(absNixFile, nixFileInTemp); err != nil {
		return "", fmt.Errorf("failed to copy nix file: %w", err)
	}

	// Copy Docker image tar files if they exist
	if sproutFile.DockerCompose.Enabled {
		for _, img := range sproutFile.DockerCompose.Images {
			if _, err := os.Stat(img.TarPath); err == nil {
				tarFileName := filepath.Base(img.TarPath)
				destPath := filepath.Join(tempDir, tarFileName)
				if err := n.copyFile(img.TarPath, destPath); err != nil {
					return "", fmt.Errorf("failed to copy docker image %s: %w", img.TarPath, err)
				}
				// Update the tar path in the nix file to the container path
				containerTarPath := filepath.Join("/workspace", tarFileName)
				if err := n.updateTarPathInNixFile(nixFileInTemp, img.TarPath, containerTarPath); err != nil {
					return "", fmt.Errorf("failed to update tar path in nix file: %w", err)
				}
			}
		}
	}

	// Show progress indication during build
	fmt.Printf("      \033[36mThis may take 2-8 minutes (optimized with parallel builds)...\033[0m\n")
	fmt.Printf("      \033[36mBuilding with Docker Linux container (4GB RAM, multi-core)...\033[0m\n")
	fmt.Printf("      \033[36mUsing persistent Nix store cache for faster subsequent builds...\033[0m\n")

	// Use Docker client to run nix-build inside container
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "", fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer cli.Close()

	// Create container configuration - run nix-build directly
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

	// Get the config folder and create nix cache directory
	configFolder, err := configpkg.Folder()
	if err != nil {
		return "", fmt.Errorf("failed to get config folder: %w", err)
	}
	nixCacheDir := filepath.Join(configFolder, "cache")
	if err := os.MkdirAll(nixCacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create nix cache directory: %w", err)
	}

	// Create Nix store directory for persistent storage
	nixStoreDir := filepath.Join(configFolder, "nix-store")
	if err := os.MkdirAll(nixStoreDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create nix store directory: %w", err)
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
		// Allocate more resources for faster builds
		Resources: container.Resources{
			CPUShares: 1024,       // Higher CPU priority
			Memory:    4294967296, // 4GB RAM limit
		},
		Privileged: true, // Required for some Nix operations
	}

	// Create and start container
	resp, err := cli.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "")
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	// Start container
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	// Stream container logs in real-time
	logs, err := cli.ContainerLogs(ctx, resp.ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return "", fmt.Errorf("failed to get container logs: %w", err)
	}

	// Capture build output to get the Nix store path
	var buildOutput strings.Builder
	go func() {
		defer finishStreamingDisplay()

		for {
			// Read Docker log header (8 bytes)
			header := make([]byte, 8)
			n, err := io.ReadFull(logs, header)
			if err != nil || n != 8 {
				break
			}

			// Extract payload size from header (bytes 4-7, big endian)
			payloadSize := int(header[4])<<24 | int(header[5])<<16 | int(header[6])<<8 | int(header[7])

			if payloadSize <= 0 {
				continue
			}

			// Read the payload
			payload := make([]byte, payloadSize)
			n, err = io.ReadFull(logs, payload)
			if err != nil || n != payloadSize {
				break
			}

			// Process payload line by line
			scanner := bufio.NewScanner(bytes.NewReader(payload))
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line != "" {
					buildOutput.WriteString(line + "\n")
					displayStreamingLine(line, "\033[36m")
				}
			}
		}
	}()

	// Wait for container to finish
	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
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

	// Extract the Nix store path from build output
	output := buildOutput.String()
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

	fmt.Printf("      \033[32mDocker build completed: %s\033[0m\n", nixStorePath)

	return nixStorePath, nil
}

func (n *Nix) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

func (n *Nix) updateTarPathInNixFile(nixFile, oldPath, newPath string) error {
	content, err := os.ReadFile(nixFile)
	if err != nil {
		return err
	}

	updatedContent := strings.ReplaceAll(string(content), oldPath, newPath)
	return os.WriteFile(nixFile, []byte(updatedContent), 0644)
}

// Unified streaming display system
var (
	displayLines       = make([]string, 4)
	displayIndex       = 0
	displayMutex       sync.Mutex
	displayInitialized = false
)

func displayStreamingLine(line, color string) {
	displayMutex.Lock()
	defer displayMutex.Unlock()

	if line == "" {
		return
	}

	// Initialize display area on first line
	if !displayInitialized {
		fmt.Printf("      %s%s\033[0m\n", color, "")
		fmt.Printf("      %s%s\033[0m\n", color, "")
		fmt.Printf("      %s%s\033[0m\n", color, "")
		fmt.Printf("      %s%s\033[0m\n", color, "")
		fmt.Print("\033[4A") // Move cursor back up 4 lines
		displayInitialized = true
	}

	// Add the new line to our rolling buffer
	displayLines[displayIndex] = line
	displayIndex = (displayIndex + 1) % 4

	// Redraw all 4 lines in their fixed positions
	for i := 0; i < 4; i++ {
		lineIndex := (displayIndex + i) % 4
		displayLine := ""
		if displayLines[lineIndex] != "" {
			displayLine = displayLines[lineIndex]
			// Truncate line if too long (assuming 80 char terminal width)
			if len(displayLine) > 76 {
				displayLine = displayLine[:73] + "..."
			}
		}
		fmt.Printf("\033[K      %s%s\033[0m\n", color, displayLine)
	}
	fmt.Print("\033[4A") // Move cursor back up 4 lines
}

func finishStreamingDisplay() {
	displayMutex.Lock()
	defer displayMutex.Unlock()

	// Move cursor down past our output area when done
	if displayInitialized {
		fmt.Print("\033[4B")
		// Reset for next use
		displayInitialized = false
		displayIndex = 0
		for i := range displayLines {
			displayLines[i] = ""
		}
	}
}
