package config

import (
	"os"
	"path/filepath"
	"runtime"
)

func Folder() (string, error) {
	var configPath string
	switch runtime.GOOS {
	case "windows":
		configPath = os.Getenv("APPDATA")
	default:
		configPath = os.Getenv("XDG_CONFIG_HOME")
	}
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configPath = filepath.Join(home, ".config")
	}
	return filepath.Join(configPath, "sprout"), nil
}
