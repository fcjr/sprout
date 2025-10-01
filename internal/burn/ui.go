package burn

import (
	"fmt"
	"os"
	"time"
)

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

func PrintBurnHeader(imagePath string, imageInfo os.FileInfo) {
	fmt.Printf("\n%s%s🔥 Sprout SD Card Burner%s\n", Bold, Yellow, Reset)
	fmt.Printf("%s%s═══════════════════════════%s\n\n", Bold, Yellow, Reset)
	fmt.Printf("%sImage file: %s%s%s\n", Bold, Green, imagePath, Reset)
	fmt.Printf("%sImage size: %s%s\n\n", Bold, FormatBytes(imageInfo.Size()), Reset)
}

func DisplayDisks(disks []DiskInfo) {
	fmt.Printf("\n%sAvailable storage devices:%s\n", Bold, Reset)
	for i, disk := range disks {
		status := ""
		if disk.Mountpoint != "" {
			status = fmt.Sprintf(" (mounted at %s)", disk.Mountpoint)
		}
		fmt.Printf("  %s%d.%s %s - %s - %s%s%s\n",
			Cyan, i+1, Reset, disk.Device, disk.Size, disk.Name, status, Reset)
	}
}

func FormatBytes(bytes int64) string {
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

func FormatDuration(d time.Duration) string {
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
