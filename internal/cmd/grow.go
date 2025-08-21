package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fcjr/sprout/nix"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(growCmd)
}

var growCmd = &cobra.Command{
	Use:   "grow",
	Short: "Grow a NixOS image from sprout.yaml configuration",
	RunE:  runGrow,
}

func runGrow(cmd *cobra.Command, args []string) error {
	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	// Read sprout.yaml from current directory
	sproutFile := filepath.Join(cwd, "sprout.yaml")
	if _, err := os.Stat(sproutFile); os.IsNotExist(err) {
		return fmt.Errorf("sprout.yaml not found in current directory")
	}

	// Load configuration from YAML
	nixInstance := &nix.Nix{}
	config, err := nixInstance.LoadConfigFromYAML(sproutFile)
	if err != nil {
		return fmt.Errorf("failed to load configuration from sprout.yaml: %w", err)
	}

	// Generate the Nix configuration
	nixConfig, err := nixInstance.GenerateImage(*config)
	if err != nil {
		return fmt.Errorf("failed to generate Nix configuration: %w", err)
	}

	// Create temporary file
	tempFile, err := os.CreateTemp("", "image-*.nix")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer tempFile.Close()

	// Write the generated configuration to temp file
	if _, err := tempFile.WriteString(nixConfig); err != nil {
		return fmt.Errorf("failed to write to temporary file: %w", err)
	}

	// Store the temp file name in a variable
	tempFileName := tempFile.Name()

	fmt.Printf("Generated Nix configuration written to: %s\n", tempFileName)

	// Build the Nix configuration
	fmt.Println("Building NixOS image...")
	imagePath, err := nixInstance.Build(tempFileName)
	if err != nil {
		return fmt.Errorf("failed to build Nix configuration: %w", err)
	}

	fmt.Printf("NixOS image built successfully: %s\n", imagePath)

	// Set default output path if not specified
	outputPath := config.Output.Path
	if outputPath == "" {
		outputPath = "build/image.img"
	}

	// If relative path, make it relative to current working directory
	if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(cwd, outputPath)
	}

	fmt.Printf("Copying image to: %s\n", outputPath)

	// Ensure the output directory exists
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Find the actual image file inside the directory
	actualImagePath, err := findImageFile(imagePath)
	if err != nil {
		return fmt.Errorf("failed to find image file: %w", err)
	}

	// Copy the actual image file
	if err := copyFile(actualImagePath, outputPath); err != nil {
		return fmt.Errorf("failed to copy image to output path: %w", err)
	}

	fmt.Printf("Image copied successfully to: %s\n", outputPath)

	return nil
}

func findImageFile(basePath string) (string, error) {
	sdImageDir := filepath.Join(basePath, "sd-image")

	// Check if sd-image directory exists
	if _, err := os.Stat(sdImageDir); os.IsNotExist(err) {
		return "", fmt.Errorf("sd-image directory not found in %s", basePath)
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

func copyFile(src, dst string) error {
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
