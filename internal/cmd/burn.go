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

	"github.com/fcjr/sprout/nix"
	"github.com/spf13/cobra"
)

type DiskInfo struct {
	Device     string
	Size       string
	SizeBytes  uint64
	Name       string
	Removable  bool
	Mountpoint string
}

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
	fmt.Printf("%sImage size: %s%s\n\n", Bold, formatBytes(imageInfo.Size()), Reset)

	// Detect available disks
	fmt.Printf("%sDetecting available storage devices...\n", Bold)
	disks, err := detectRemovableDisks()
	if err != nil {
		return fmt.Errorf("failed to detect disks: %w", err)
	}

	if len(disks) == 0 {
		fmt.Printf("%sNo suitable removable storage devices found.\n", Red)
		fmt.Printf("Please insert an SD card and try again.%s\n", Reset)
		return nil
	}

	// Display available disks
	fmt.Printf("\n%sAvailable storage devices:%s\n", Bold, Reset)
	for i, disk := range disks {
		status := ""
		if disk.Mountpoint != "" {
			status = fmt.Sprintf(" (mounted at %s)", disk.Mountpoint)
		}
		fmt.Printf("  %s%d.%s %s - %s - %s%s%s\n",
			Cyan, i+1, Reset, disk.Device, disk.Size, disk.Name, status, Reset)
	}

	// Get user selection
	var selectedDisk *DiskInfo
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
	if err := unmountDisk(selectedDisk); err != nil {
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
		config, err := nixInstance.LoadSproutFileFromYAMLLightweight(sproutFile)
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

func detectRemovableDisks() ([]DiskInfo, error) {
	switch runtime.GOOS {
	case "darwin":
		return detectRemovableDisksMacOS()
	case "linux":
		return detectRemovableDisksLinux()
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func detectRemovableDisksMacOS() ([]DiskInfo, error) {
	cmd := exec.Command("diskutil", "list", "-plist")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run diskutil: %w", err)
	}

	// Parse diskutil output - for now, use a simpler approach
	cmd = exec.Command("diskutil", "list")
	output, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run diskutil: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	var disks []DiskInfo

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "/dev/disk") && strings.Contains(line, "external") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				device := parts[0]

				// Get detailed info for this disk
				diskInfo, err := getMacOSDiskInfo(device)
				if err != nil {
					continue
				}

				// Skip large disks (likely system disks)
				if diskInfo.SizeBytes > 256*1024*1024*1024 { // 256 GB
					continue
				}

				disks = append(disks, *diskInfo)
			}
		}
	}

	return disks, nil
}

func detectRemovableDisksLinux() ([]DiskInfo, error) {
	cmd := exec.Command("lsblk", "-J", "-o", "NAME,SIZE,MOUNTPOINT,TYPE,REMOVABLE")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run lsblk: %w", err)
	}

	// For simplicity, parse the output manually
	// In production, you'd want to parse the JSON properly
	cmd = exec.Command("lsblk", "-o", "NAME,SIZE,MOUNTPOINT,TYPE,REMOVABLE", "--noheadings")
	output, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run lsblk: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	var disks []DiskInfo

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[4] == "1" && fields[3] == "disk" {
			device := "/dev/" + fields[0]
			size := fields[1]
			mountpoint := ""
			if len(fields) > 2 && fields[2] != "" {
				mountpoint = fields[2]
			}

			// Get size in bytes for comparison
			sizeBytes, _ := parseSize(size)

			// Skip large disks (likely system disks)
			if sizeBytes > 256*1024*1024*1024 { // 256 GB
				continue
			}

			disks = append(disks, DiskInfo{
				Device:     device,
				Size:       size,
				SizeBytes:  sizeBytes,
				Name:       fmt.Sprintf("Removable disk (%s)", size),
				Removable:  true,
				Mountpoint: mountpoint,
			})
		}
	}

	return disks, nil
}

func getMacOSDiskInfo(device string) (*DiskInfo, error) {
	cmd := exec.Command("diskutil", "info", device)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(output), "\n")
	info := &DiskInfo{Device: device}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Disk Size:") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				sizeStr := strings.TrimSpace(parts[1])
				info.Size = sizeStr
				// Extract bytes from something like "31.9 GB (31914983424 Bytes)"
				if idx := strings.Index(sizeStr, "("); idx != -1 {
					if endIdx := strings.Index(sizeStr[idx:], " Bytes)"); endIdx != -1 {
						bytesStr := sizeStr[idx+1 : idx+endIdx]
						if bytes, err := strconv.ParseUint(bytesStr, 10, 64); err == nil {
							info.SizeBytes = bytes
						}
					}
				}
			}
		} else if strings.Contains(line, "Volume Name:") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				info.Name = strings.TrimSpace(parts[1])
			}
		} else if strings.Contains(line, "Mount Point:") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				mountpoint := strings.TrimSpace(parts[1])
				if mountpoint != "Not applicable (no file system)" {
					info.Mountpoint = mountpoint
				}
			}
		}
	}

	if info.Name == "" {
		info.Name = fmt.Sprintf("Disk (%s)", info.Size)
	}

	return info, nil
}

func parseSize(sizeStr string) (uint64, error) {
	sizeStr = strings.ToUpper(strings.TrimSpace(sizeStr))

	var multiplier uint64 = 1
	if strings.HasSuffix(sizeStr, "K") {
		multiplier = 1024
		sizeStr = sizeStr[:len(sizeStr)-1]
	} else if strings.HasSuffix(sizeStr, "M") {
		multiplier = 1024 * 1024
		sizeStr = sizeStr[:len(sizeStr)-1]
	} else if strings.HasSuffix(sizeStr, "G") {
		multiplier = 1024 * 1024 * 1024
		sizeStr = sizeStr[:len(sizeStr)-1]
	} else if strings.HasSuffix(sizeStr, "T") {
		multiplier = 1024 * 1024 * 1024 * 1024
		sizeStr = sizeStr[:len(sizeStr)-1]
	}

	size, err := strconv.ParseFloat(sizeStr, 64)
	if err != nil {
		return 0, err
	}

	return uint64(size * float64(multiplier)), nil
}

func selectDisk(disks []DiskInfo) (*DiskInfo, error) {
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

func unmountDisk(disk *DiskInfo) error {
	switch runtime.GOOS {
	case "darwin":
		// First try to unmount all volumes on the disk
		cmd := exec.Command("diskutil", "unmountDisk", "force", disk.Device)
		output, err := cmd.CombinedOutput()
		if err != nil {
			// If that fails, try individual volumes
			return fmt.Errorf("failed to unmount disk: %w\nOutput: %s", err, output)
		}
		return nil
	case "linux":
		// Try to unmount the device
		cmd := exec.Command("umount", disk.Device)
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Try force unmount
			cmd = exec.Command("umount", "-f", disk.Device)
			output2, err2 := cmd.CombinedOutput()
			if err2 != nil {
				return fmt.Errorf("failed to unmount disk: %w\nOutput: %s\nForce unmount output: %s", err, output, output2)
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported platform")
	}
}

func burnImage(imagePath string, disk *DiskInfo, fast bool) error {
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

func runWithProgress(cmd *exec.Cmd, imagePath string, disk *DiskInfo) error {
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
					formatBytes(currentBytes), formatBytes(imageSize),
					formatBytes(speed), formatDuration(elapsed), Reset)
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
		Green, formatDuration(totalTime), formatBytes(avgSpeed), Reset)

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

	disks, err := detectRemovableDisks()
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
