package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fcjr/sprout/internal/burn"
	"github.com/fcjr/sprout/internal/nix"
	"github.com/spf13/cobra"
)

var burnCmd = &cobra.Command{
	Use:   "burn [image-file]",
	Short: "Burn/flash a Sprout image to an SD card",
	Long: `Interactive tool to safely burn a Sprout image to an SD card.
Automatically detects removable storage devices and provides safety checks
to prevent accidentally overwriting system disks.

If no image file is specified, it will look for the most recent .img file
in the current directory.`,
	RunE: runBurn,
}

func init() {
	rootCmd.AddCommand(burnCmd)
	burnCmd.Flags().Bool("force", false, "Skip confirmation prompts (use with caution)")
	burnCmd.Flags().Bool("list-disks", false, "List available disks and exit")
	burnCmd.Flags().Bool("fast", false, "Use fastest settings (less safe, skip verification)")
}

func runBurn(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	listDisks, _ := cmd.Flags().GetBool("list-disks")
	fast, _ := cmd.Flags().GetBool("fast")

	// Check if we're running on a supported platform
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return fmt.Errorf("burn command is currently only supported on macOS and Linux")
	}

	// List disks and exit if requested
	if listDisks {
		return listAvailableDisks()
	}

	// Find the image file
	imagePath, err := findImageFile(args)
	if err != nil {
		return err
	}

	fmt.Printf("\n%s%s🔥 Sprout SD Card Burner%s\n", Bold, Yellow, Reset)
	fmt.Printf("%s%s═══════════════════════════%s\n\n", Bold, Yellow, Reset)

	fmt.Printf("%sImage file: %s%s%s\n", Bold, Green, imagePath, Reset)

	// Check if image file exists
	if _, err := os.Stat(imagePath); os.IsNotExist(err) {
		return fmt.Errorf("image file not found: %s", imagePath)
	}

	// Get image file size
	imageInfo, err := os.Stat(imagePath)
	if err != nil {
		return fmt.Errorf("failed to get image file info: %w", err)
	}
	fmt.Printf("%sImage size: %s%s\n\n", Bold, burn.FormatBytes(imageInfo.Size()), Reset)

	// Detect available disks
	fmt.Printf("%sDetecting available storage devices...\n", Bold)
	disks, err := burn.DetectRemovableDisks()
	if err != nil {
		return fmt.Errorf("failed to detect disks: %w", err)
	}

	if len(disks) == 0 {
		fmt.Printf("%sNo suitable removable storage devices found.\n", Red)
		fmt.Printf("Please insert an SD card and try again.%s\n", Reset)
		return nil
	}

	// Display available disks
	burn.DisplayDisks(disks)

	// Get user selection
	var selectedDisk *burn.DiskInfo
	if !force {
		selectedDisk, err = selectDisk(disks)
		if err != nil {
			return err
		}
	} else {
		if len(disks) == 1 {
			selectedDisk = &disks[0]
		} else {
			return fmt.Errorf("multiple disks available, cannot use --force without manual selection")
		}
	}

	// Final confirmation
	if !force {
		fmt.Printf("\n%s⚠️  WARNING: This will completely erase all data on %s (%s)%s\n",
			Red, selectedDisk.Device, selectedDisk.Name, Reset)
		fmt.Printf("%sDo you want to continue? (yes/no): %s", Bold, Reset)

		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		response = strings.TrimSpace(strings.ToLower(response))
		if response != "yes" && response != "y" {
			fmt.Printf("Operation cancelled.\n")
			return nil
		}
	}

	// Always try to unmount the disk before burning
	fmt.Printf("\n%sUnmounting %s...\n", Bold, selectedDisk.Device)
	if err := burn.UnmountDisk(selectedDisk); err != nil {
		fmt.Printf("%sWarning: failed to unmount disk: %v%s\n", Yellow, err, Reset)
		fmt.Printf("%sAttempting to continue anyway...%s\n", Yellow, Reset)
	} else {
		fmt.Printf("%s✓ Disk unmounted successfully%s\n", Green, Reset)
	}

	// Burn the image
	fmt.Printf("\n%sBurning image to %s...\n", Bold, selectedDisk.Device)
	if fast {
		fmt.Printf("%sUsing fast mode (8MB blocks, minimal sync)...%s\n", Yellow, Reset)
	} else {
		fmt.Printf("%sUsing safe mode (8MB blocks, full sync)...%s\n", Yellow, Reset)
	}
	fmt.Printf("%sThis may take several minutes...%s\n\n", Yellow, Reset)

	if err := burnImage(imagePath, selectedDisk, fast); err != nil {
		return fmt.Errorf("failed to burn image: %w", err)
	}

	fmt.Printf("\n%s%s✅ Successfully burned image to %s!%s\n", Bold, Green, selectedDisk.Device, Reset)
	fmt.Printf("%sYou can now safely remove the SD card and use it in your device.%s\n", Green, Reset)

	return nil
}

func findImageFile(args []string) (string, error) {
	if len(args) > 0 {
		return filepath.Abs(args[0])
	}

	// First, try to read sprout.yaml to get the configured output path
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %w", err)
	}

	sproutFile := filepath.Join(cwd, "sprout.yaml")
	if _, err := os.Stat(sproutFile); err == nil {
		// sprout.yaml exists, try to load it (lightweight - no Docker processing)
		nixInstance := &nix.Nix{}
		config, err := nixInstance.LoadConfigOnly(sproutFile)
		if err != nil {
			fmt.Printf("%sWarning: Found sprout.yaml but failed to load it: %v%s\n", Yellow, err, Reset)
		} else if config.Output.Path != "" {
			// Check if the configured output path exists
			outputPath := config.Output.Path
			if !filepath.IsAbs(outputPath) {
				outputPath = filepath.Join(cwd, outputPath)
			}

			if _, err := os.Stat(outputPath); err == nil {
				fmt.Printf("%sUsing image from sprout.yaml: %s%s\n", Green, outputPath, Reset)
				return filepath.Abs(outputPath)
			} else {
				// Image file doesn't exist, suggest building it first
				return "", fmt.Errorf("image file not found at configured path: %s\n"+
					"Hint: Run 'sprout seed' to build the image first, or specify an image file directly", outputPath)
			}
		}
	}

	// Fallback: Look for .img files in current directory
	entries, err := os.ReadDir(".")
	if err != nil {
		return "", fmt.Errorf("failed to read current directory: %w", err)
	}

	var imgFiles []os.DirEntry
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".img") {
			imgFiles = append(imgFiles, entry)
		}
	}

	if len(imgFiles) == 0 {
		return "", fmt.Errorf("no sprout.yaml found and no .img files found in current directory\n" +
			"Hint: Create a sprout.yaml file and run 'sprout seed' to build an image, or specify an image file directly")
	}

	if len(imgFiles) == 1 {
		fmt.Printf("%sUsing found image file: %s%s\n", Green, imgFiles[0].Name(), Reset)
		return filepath.Abs(imgFiles[0].Name())
	}

	// Multiple files found, let user choose
	fmt.Printf("Multiple .img files found:\n")
	for i, file := range imgFiles {
		fmt.Printf("  %d. %s\n", i+1, file.Name())
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Select image file (1-%d): ", len(imgFiles))
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	choice, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil || choice < 1 || choice > len(imgFiles) {
		return "", fmt.Errorf("invalid selection")
	}

	return filepath.Abs(imgFiles[choice-1].Name())
}

func selectDisk(disks []burn.DiskInfo) (*burn.DiskInfo, error) {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Printf("\n%sSelect a disk to burn to (1-%d, or 'q' to quit): %s", Bold, len(disks), Reset)
		input, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "q" || input == "quit" {
			return nil, fmt.Errorf("operation cancelled")
		}

		choice, err := strconv.Atoi(input)
		if err != nil || choice < 1 || choice > len(disks) {
			fmt.Printf("%sInvalid selection. Please enter a number between 1 and %d.%s\n", Red, len(disks), Reset)
			continue
		}

		return &disks[choice-1], nil
	}
}

func burnImage(imagePath string, disk *burn.DiskInfo, fast bool) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		if fast {
			// Ultra-fast mode: 16MB blocks, no sync during write
			cmd = exec.Command("sudo", "dd",
				fmt.Sprintf("if=%s", imagePath),
				fmt.Sprintf("of=%s", disk.Device),
				"bs=16m",       // 16MB blocks for maximum speed
				"conv=notrunc") // No sync during write
		} else {
			// Safe mode: 8MB blocks with sync
			cmd = exec.Command("sudo", "dd",
				fmt.Sprintf("if=%s", imagePath),
				fmt.Sprintf("of=%s", disk.Device),
				"bs=8m",        // 8MB blocks
				"conv=notrunc", // Don't truncate
				"oflag=sync")   // Sync writes for reliability
		}
	case "linux":
		if fast {
			// Ultra-fast mode: 16MB blocks, no metadata sync
			cmd = exec.Command("sudo", "dd",
				fmt.Sprintf("if=%s", imagePath),
				fmt.Sprintf("of=%s", disk.Device),
				"bs=16M",       // 16MB blocks for maximum speed
				"conv=notrunc", // No conversion, faster
				"oflag=direct", // Direct I/O, bypasses cache
				"status=progress")
		} else {
			// Safe mode: 8MB blocks with data sync
			cmd = exec.Command("sudo", "dd",
				fmt.Sprintf("if=%s", imagePath),
				fmt.Sprintf("of=%s", disk.Device),
				"bs=8M",          // 8MB blocks
				"conv=fdatasync", // Only sync data, not metadata
				"status=progress")
		}
	default:
		return fmt.Errorf("unsupported platform")
	}

	cmd.Stdin = os.Stdin // For sudo password prompt

	// Run the dd command with progress monitoring
	err := runWithProgress(cmd, imagePath, disk)
	if err != nil {
		return fmt.Errorf("failed to burn image: %w\n"+
			"This could be due to:\n"+
			"- Insufficient permissions (try running with sudo)\n"+
			"- Device still busy/mounted\n"+
			"- Hardware write protection on the SD card\n"+
			"- Corrupted image file", err)
	}

	// Sync to ensure all data is written
	fmt.Printf("\n%sSyncing filesystem...%s\n", Bold, Reset)
	var syncCmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		syncCmd = exec.Command("sync")
	case "linux":
		syncCmd = exec.Command("sync")
	}

	if syncCmd != nil {
		if syncErr := syncCmd.Run(); syncErr != nil {
			fmt.Printf("%sWarning: sync failed: %v%s\n", Yellow, syncErr, Reset)
		} else {
			fmt.Printf("%s✓ Filesystem synced%s\n", Green, Reset)
		}
	}

	return nil
}

func runWithProgress(cmd *exec.Cmd, imagePath string, disk *burn.DiskInfo) error {
	// Get image size for progress calculation
	imageInfo, err := os.Stat(imagePath)
	if err != nil {
		return fmt.Errorf("failed to get image size: %w", err)
	}
	imageSize := imageInfo.Size()

	// For Linux, we can use the built-in status=progress
	if runtime.GOOS == "linux" {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// For macOS, provide our own progress monitoring
	// Redirect stdout/stderr to capture dd output
	cmd.Stdout = nil
	cmd.Stderr = os.Stderr

	startTime := time.Now()
	if err := cmd.Start(); err != nil {
		return err
	}

	// Monitor progress in a separate goroutine
	var wg sync.WaitGroup
	progressDone := make(chan bool, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-progressDone:
				// Clear the progress line
				fmt.Printf("\r\033[K")
				return
			case <-ticker.C:
				elapsed := time.Since(startTime)

				// Try to get current bytes written by checking device size
				// This is approximate but gives users feedback
				currentBytes := getApproximateBytesWritten(disk.Device, elapsed, imageSize)

				// Calculate speed
				speed := int64(0)
				if elapsed.Seconds() > 0 {
					speed = currentBytes / int64(elapsed.Seconds())
				}

				// Calculate percentage
				percentage := float64(currentBytes) / float64(imageSize) * 100
				if percentage > 100 {
					percentage = 100
				}

				// Show progress
				fmt.Printf("\r%s⏳ Burning... %.1f%% (%s / %s) %s/s - %s elapsed%s",
					Bold, percentage,
					burn.FormatBytes(currentBytes), burn.FormatBytes(imageSize),
					burn.FormatBytes(speed), burn.FormatDuration(elapsed), Reset)
			}
		}
	}()

	// Wait for dd to complete
	err = cmd.Wait()
	totalTime := time.Since(startTime)

	// Signal progress monitoring to stop
	progressDone <- true
	wg.Wait()

	// Show completion message
	avgSpeed := int64(0)
	if totalTime.Seconds() > 0 {
		avgSpeed = imageSize / int64(totalTime.Seconds())
	}

	fmt.Printf("%s✓ Burn completed in %s (avg: %s/s)%s\n",
		Green, burn.FormatDuration(totalTime), burn.FormatBytes(avgSpeed), Reset)

	return err
}

func getApproximateBytesWritten(device string, elapsed time.Duration, totalSize int64) int64 {
	// This is a rough estimation based on elapsed time
	// In reality, we'd need more sophisticated monitoring to get exact bytes
	// But this gives users a sense of progress

	// Assume roughly linear progress (which isn't accurate but better than nothing)
	// We'll be conservative and assume it takes longer than it actually does
	estimatedTotalTime := 300 * time.Second // Assume 5 minutes for a typical image

	if elapsed >= estimatedTotalTime {
		return totalSize
	}

	progress := float64(elapsed) / float64(estimatedTotalTime)
	if progress > 1.0 {
		progress = 1.0
	}

	return int64(float64(totalSize) * progress)
}

func listAvailableDisks() error {
	fmt.Printf("Available storage devices:\n\n")

	disks, err := burn.DetectRemovableDisks()
	if err != nil {
		return fmt.Errorf("failed to detect disks: %w", err)
	}

	if len(disks) == 0 {
		fmt.Printf("No removable storage devices found.\n")
		return nil
	}

	for _, disk := range disks {
		status := ""
		if disk.Mountpoint != "" {
			status = fmt.Sprintf(" (mounted at %s)", disk.Mountpoint)
		}
		fmt.Printf("  %s - %s - %s%s\n", disk.Device, disk.Size, disk.Name, status)
	}

	return nil
}
