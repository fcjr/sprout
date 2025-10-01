package burn

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

func DetectRemovableDisks() ([]DiskInfo, error) {
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

				diskInfo, err := getMacOSDiskInfo(device)
				if err != nil {
					continue
				}

				if diskInfo.SizeBytes > 256*1024*1024*1024 {
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

			sizeBytes, _ := parseSize(size)

			if sizeBytes > 256*1024*1024*1024 {
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
		parts := strings.Split(line, ":")
		if len(parts) <= 1 {
			continue
		}

		value := strings.TrimSpace(parts[1])

		if strings.Contains(line, "Disk Size:") {
			info.Size = value
			idx := strings.Index(value, "(")
			endIdx := strings.Index(value[idx:], " Bytes)")
			if idx != -1 && endIdx != -1 {
				bytesStr := value[idx+1 : idx+endIdx]
				if bytes, err := strconv.ParseUint(bytesStr, 10, 64); err == nil {
					info.SizeBytes = bytes
				}
			}
		} else if strings.Contains(line, "Volume Name:") {
			info.Name = value
		} else if strings.Contains(line, "Mount Point:") && value != "Not applicable (no file system)" {
			info.Mountpoint = value
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

func UnmountDisk(disk *DiskInfo) error {
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("diskutil", "unmountDisk", "force", disk.Device)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to unmount disk: %w\nOutput: %s", err, output)
		}
		return nil
	case "linux":
		cmd := exec.Command("umount", disk.Device)
		output, err := cmd.CombinedOutput()
		if err != nil {
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
