# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

- **Build**: `go build cmd/sprout/main.go` or `go build -o sprout cmd/sprout/main.go`
- **Run**: `go run cmd/sprout/main.go [command]`
- **Test**: `go test ./...`
- **Format**: `go fmt ./...`
- **Lint**: `go vet ./...`
- **Generate completions**: `./scripts/completions.sh` (creates shell completions in `completions/` dir)
- **Generate man pages**: `./scripts/manpages.sh` (creates compressed man page in `manpages/` dir)

## Architecture

Sprout is a CLI tool that generates NixOS SD card images from YAML configuration files. The architecture follows a standard Go CLI pattern:

### Core Components

- **CLI Layer** (`cmd/sprout/main.go`, `internal/cmd/`): Built with Cobra framework
  - `root.go`: Base command with 15-second timeout, version handling
  - `grow.go`: Main command that processes `sprout.yaml` files and builds NixOS images
  - `man.go`: Man page generation command

- **Nix Integration** (`nix/nix.go`, `nix/image.nix.tmpl`):
  - Loads YAML configuration defining SSH keys, wireless networks, and output paths
  - Uses embedded Nix template to generate NixOS configurations for ARM64 systems
  - Executes `nix-build` to create bootable SD card images
  - Handles copying built images from Nix store to specified output location

- **Configuration** (`internal/config/config.go`): Defines TOML-based server configuration (currently unused by main workflow)

### Workflow

1. User creates `sprout.yaml` with SSH keys, wireless config, and output path
2. `sprout grow` command loads the YAML configuration
3. Nix template is populated with user configuration
4. `nix-build` creates ARM64 NixOS installer image with SSH access enabled
5. Built image is copied from Nix store to user-specified location (default: `build/image.img`)

### Key Dependencies

- `github.com/spf13/cobra`: CLI framework
- `gopkg.in/yaml.v3`: YAML configuration parsing  
- `text/template`: Nix configuration templating
- Requires `nix` command-line tools for building images