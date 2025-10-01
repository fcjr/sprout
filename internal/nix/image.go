package nix

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func FindImageFile(basePath string) (string, error) {
	fileInfo, err := os.Stat(basePath)
	if err != nil {
		return "", fmt.Errorf("path does not exist: %s", basePath)
	}

	if !fileInfo.IsDir() && filepath.Ext(basePath) == ".img" {
		return basePath, nil
	}

	if strings.Contains(basePath, "sprout-docker-") {
		isResultImg := filepath.Base(basePath) == "result.img"
		if isResultImg && !fileInfo.IsDir() {
			return basePath, nil
		}

		resultImg := filepath.Join(basePath, "result.img")
		if _, err := os.Stat(resultImg); err == nil {
			return resultImg, nil
		}
		return "", fmt.Errorf("docker build completed but image file not found at %s", resultImg)
	}

	sdImageDir := filepath.Join(basePath, "sd-image")

	if _, err := os.Stat(sdImageDir); os.IsNotExist(err) {
		return "", fmt.Errorf("image file not found: neither %s nor %s/sd-image/*.img exists", basePath, basePath)
	}

	entries, err := os.ReadDir(sdImageDir)
	if err != nil {
		return "", fmt.Errorf("failed to read sd-image directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".img" {
			return filepath.Join(sdImageDir, entry.Name()), nil
		}
	}

	return "", fmt.Errorf("no .img file found in sd-image directory")
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
