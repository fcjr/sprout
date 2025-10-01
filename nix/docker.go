package nix

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"gopkg.in/yaml.v3"
)

func (n *Nix) processDockerComposeImages(dockerConfig *DockerComposeConfig, composePath string) error {
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

	var images []DockerImage
	imageMap := make(map[string]bool)

	for _, service := range project.Services {
		var imageName string

		if service.Image != "" {
			imageName = service.Image
		} else if service.Build != nil {
			imageName = fmt.Sprintf("%s:latest", service.Name)
		}

		if imageName != "" && !imageMap[imageName] {
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

	err = n.createModifiedComposeContent(dockerConfig, project)
	if err != nil {
		return fmt.Errorf("failed to create modified compose content: %w", err)
	}

	err = n.buildAndSaveDockerImages(dockerConfig, filepath.Dir(composePath))
	if err != nil {
		return fmt.Errorf("failed to build and save docker images: %w", err)
	}

	return nil
}

func (n *Nix) createModifiedComposeContent(dockerConfig *DockerComposeConfig, project *types.Project) error {
	imageMapping := make(map[string]string)
	for _, img := range dockerConfig.Images {
		imageMapping[img.Name] = img.LocalTag
	}

	for serviceName, service := range project.Services {
		if service.Image != "" && imageMapping[service.Image] != "" {
			service.Image = imageMapping[service.Image]
		} else if service.Build != nil {
			localTag := fmt.Sprintf("embedded/%s_latest", serviceName)
			service.Image = localTag
			service.Build = nil
		}
		project.Services[serviceName] = service
	}

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

		if err := n.buildOrPullImage(ctx, cli, &img, workingDir); err != nil {
			return err
		}

		if err := n.tagAndSaveImage(ctx, cli, &img); err != nil {
			return err
		}

		fmt.Printf("      \033[32mSaved: %s\033[0m\n", img.LocalTag)
		dockerConfig.Images[i] = img
	}

	return nil
}

func (n *Nix) buildOrPullImage(ctx context.Context, cli *client.Client, img *DockerImage, workingDir string) error {
	if strings.HasSuffix(img.Name, ":latest") && !strings.Contains(img.Name, "/") {
		return n.buildLocalImage(img.Name, workingDir)
	}
	return n.pullImage(ctx, cli, img.Name)
}

func (n *Nix) buildLocalImage(imageName, workingDir string) error {
	serviceName := strings.TrimSuffix(imageName, ":latest")
	cmd := exec.Command("docker-compose", "build", serviceName)
	cmd.Dir = workingDir
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build service %s: %w", serviceName, err)
	}
	return nil
}

func (n *Nix) pullImage(ctx context.Context, cli *client.Client, imageName string) error {
	pullOptions := image.PullOptions{
		Platform: "linux/arm64",
	}
	reader, err := cli.ImagePull(ctx, imageName, pullOptions)
	if err != nil {
		return n.pullDefaultImage(ctx, cli, imageName)
	}

	fmt.Printf("      Successfully pulled ARM64 image: %s\n", imageName)
	io.Copy(io.Discard, reader)
	reader.Close()
	return nil
}

func (n *Nix) pullDefaultImage(ctx context.Context, cli *client.Client, imageName string) error {
	fmt.Printf("      ARM64 image not available, trying default platform: %s\n", imageName)
	pullOptions := image.PullOptions{}
	reader, err := cli.ImagePull(ctx, imageName, pullOptions)
	if err != nil {
		fmt.Printf("Warning: failed to pull image %s, assuming it exists locally: %v\n", imageName, err)
		return nil
	}
	io.Copy(io.Discard, reader)
	reader.Close()
	return nil
}

func (n *Nix) tagAndSaveImage(ctx context.Context, cli *client.Client, img *DockerImage) error {
	err := cli.ImageTag(ctx, img.Name, img.LocalTag)
	if err != nil {
		return fmt.Errorf("failed to tag image %s as %s: %w", img.Name, img.LocalTag, err)
	}

	reader, err := cli.ImageSave(ctx, []string{img.LocalTag})
	if err != nil {
		return fmt.Errorf("failed to save image %s: %w", img.LocalTag, err)
	}
	defer reader.Close()

	return n.saveImageToFile(reader, img.TarPath)
}

func (n *Nix) saveImageToFile(reader io.ReadCloser, tarPath string) error {
	tarFile, err := os.Create(tarPath)
	if err != nil {
		return fmt.Errorf("failed to create tar file %s: %w", tarPath, err)
	}
	defer tarFile.Close()

	_, err = io.Copy(tarFile, reader)
	if err != nil {
		return fmt.Errorf("failed to write image to tar file %s: %w", tarPath, err)
	}
	return nil
}
