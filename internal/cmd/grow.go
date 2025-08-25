package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

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
	startTime := time.Now()
	printHeader()

	// Get current working directory
	printStep("📂 Finding current directory...")
	cwd, err := os.Getwd()
	if err != nil {
		return printError("failed to get current working directory: %w", err)
	}
	printSuccess(fmt.Sprintf("Working in %s", cwd))

	// Read sprout.yaml from current directory
	printStep("🌱 Looking for sprout.yaml...")
	sproutFile := filepath.Join(cwd, "sprout.yaml")
	if _, err := os.Stat(sproutFile); os.IsNotExist(err) {
		return printError("sprout.yaml not found in current directory")
	}
	printSuccess("Found sprout.yaml")

	// Load configuration from YAML
	printStep("📋 Loading configuration...")
	nixInstance := &nix.Nix{}
	config, err := nixInstance.LoadConfigFromYAML(sproutFile)
	if err != nil {
		return printError("failed to load configuration from sprout.yaml: %w", err)
	}
	printConfigInfo(config)

	// Generate the Nix configuration
	printStep("⚙️  Generating Nix configuration...")
	nixConfig, err := nixInstance.GenerateImage(*config)
	if err != nil {
		return printError("failed to generate Nix configuration: %w", err)
	}
	printSuccess("Nix configuration generated")

	// Create temporary file
	printStep("📝 Creating temporary Nix file...")
	tempFile, err := os.CreateTemp("", "image-*.nix")
	if err != nil {
		return printError("failed to create temporary file: %w", err)
	}
	defer tempFile.Close()

	// Write the generated configuration to temp file
	if _, err := tempFile.WriteString(nixConfig); err != nil {
		return printError("failed to write to temporary file: %w", err)
	}

	// Store the temp file name in a variable
	tempFileName := tempFile.Name()
	printSuccess(fmt.Sprintf("Configuration written to %s", tempFileName))

	// Build the Nix configuration
	printStep("🔨 Building NixOS image (this may take several minutes)...")
	printSubStep("⏳ Running nix-build...")
	buildStart := time.Now()
	imagePath, err := nixInstance.Build(tempFileName)
	if err != nil {
		return printError("failed to build Nix configuration: %w", err)
	}
	buildDuration := time.Since(buildStart)
	printSuccess(fmt.Sprintf("Image built in %v", formatDuration(buildDuration)))
	printSubStep(fmt.Sprintf("📍 Image location: %s", imagePath))

	// Set default output path if not specified
	outputPath := config.Output.Path
	if outputPath == "" {
		outputPath = "build/image.img"
	}

	// If relative path, make it relative to current working directory
	if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(cwd, outputPath)
	}

	printStep("📦 Preparing output location...")
	printSubStep(fmt.Sprintf("📍 Destination: %s", outputPath))

	// Ensure the output directory exists
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return printError("failed to create output directory: %w", err)
	}
	printSubStep("✅ Output directory ready")

	// Find the actual image file inside the directory
	printSubStep("🔍 Locating image file...")
	actualImagePath, err := findImageFile(imagePath)
	if err != nil {
		return printError("failed to find image file: %w", err)
	}
	printSubStep(fmt.Sprintf("📍 Found: %s", filepath.Base(actualImagePath)))

	// Copy the actual image file
	printStep("📋 Copying image file...")
	copyStart := time.Now()
	if err := copyFileWithProgress(actualImagePath, outputPath); err != nil {
		return printError("failed to copy image to output path: %w", err)
	}
	copyDuration := time.Since(copyStart)

	totalDuration := time.Since(startTime)
	printFinalSuccess(outputPath, totalDuration, copyDuration)
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

// Color constants
const (
	Reset  = "\033[0m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Blue   = "\033[34m"
	Purple = "\033[35m"
	Cyan   = "\033[36m"
	Bold   = "\033[1m"
)

func printHeader() {
	fmt.Printf("\n%s%s🌱 Sprout - grow ISOs from docker compose files%s\n", Bold, Green, Reset)
	fmt.Printf("%s%s═══════════════════════════════%s\n\n", Bold, Green, Reset)
}

func printStep(message string) {
	fmt.Printf("%s%s▶ %s%s\n", Bold, Blue, message, Reset)
}

func printSubStep(message string) {
	fmt.Printf("  %s%s%s\n", Cyan, message, Reset)
}

func printSuccess(message string) {
	fmt.Printf("  %s✅ %s%s\n", Green, message, Reset)
}

func printError(format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	fmt.Printf("\n%s❌ Error: %s%s\n", Red, err.Error(), Reset)
	return err
}

func printConfigInfo(config *nix.ImageConfig) {
	fmt.Printf("  %s✅ Configuration loaded%s\n", Green, Reset)
	fmt.Printf("    %s• SSH Keys: %d%s\n", Cyan, len(config.SSHKeys), Reset)
	if config.Wireless.Enabled {
		fmt.Printf("    %s• Wireless: %d network(s)%s\n", Cyan, len(config.Wireless.Networks), Reset)
	}
	if config.DockerCompose.Enabled {
		fmt.Printf("    %s• Docker Compose: enabled%s\n", Cyan, Reset)
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}

func printFinalSuccess(outputPath string, totalDuration, copyDuration time.Duration) {
	fmt.Printf("\n%s%s🎉 Build Complete!%s\n", Bold, Green, Reset)
	fmt.Printf("%s%s════════════════%s\n", Bold, Green, Reset)
	fmt.Printf("%s📁 Image saved to: %s%s%s\n", Bold, Green, outputPath, Reset)
	fmt.Printf("%s⏱️  Total time: %s%s\n", Bold, formatDuration(totalDuration), Reset)
	fmt.Printf("%s📋 Copy time: %s%s\n", Bold, formatDuration(copyDuration), Reset)
	fmt.Printf("\n%s💡 You can now flash this image to an SD card!%s\n", Yellow, Reset)
}

func copyFileWithProgress(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Get file size for progress tracking
	fileInfo, err := sourceFile.Stat()
	if err != nil {
		return err
	}
	fileSize := fileInfo.Size()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	// Copy with progress indication
	buffer := make([]byte, 1024*1024) // 1MB buffer
	var totalCopied int64

	printSubStep("📊 Starting copy...")

	for {
		n, err := sourceFile.Read(buffer)
		if n > 0 {
			_, writeErr := destFile.Write(buffer[:n])
			if writeErr != nil {
				return writeErr
			}
			totalCopied += int64(n)

			// Show progress every 10MB or at end
			if totalCopied%10485760 == 0 || err == io.EOF {
				percentage := float64(totalCopied) / float64(fileSize) * 100
				// Clear the line and print progress
				fmt.Printf("\r\033[K  %s📊 Progress: %.1f%% (%s / %s)%s",
					Cyan, percentage,
					formatBytes(totalCopied), formatBytes(fileSize), Reset)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	fmt.Printf("\n")
	printSuccess(fmt.Sprintf("Copied %s successfully", formatBytes(fileSize)))
	return nil
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
