package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/fcjr/sprout/nix"
	"github.com/spf13/cobra"
)

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
	
	return nil
}

func init() {
	rootCmd.AddCommand(growCmd)
}