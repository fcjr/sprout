# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

- **Build**: `go build -o sprout cmd/sprout/main.go`
- **Run**: `go run cmd/sprout/main.go [command]`
- **Test**: `go test ./...`
- **Format**: `go fmt ./...`
- **Lint**: `go vet ./...`
- **Cross-compile for ARM64**: `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o sprout cmd/sprout/main.go`
- **Generate completions**: `./scripts/completions.sh` (creates shell completions in `completions/` dir)
- **Generate man pages**: `./scripts/manpages.sh` (creates compressed man page in `manpages/` dir)

## CLI Commands

- `sprout seed` - Build NixOS image from sprout.yaml configuration
- `sprout burn <image>` - Interactive SD card flashing tool (macOS/Linux)
- `sprout discover` - Find Sprout devices on network via mDNS
- `sprout daemon` - Run mDNS discovery daemon

## Architecture

Sprout transforms Docker Compose configurations into bootable NixOS ARM64 images. It supports two build modes: local Nix installation (faster) or Docker-based builds (no Nix required).

### Core Workflow

1. **Configuration Loading** (`internal/nix/config.go`): Parse `sprout.yaml` with SSH keys, wireless networks, Docker Compose path, and output settings
2. **Docker Processing** (`internal/nix/docker.go`): If Docker Compose is enabled:
   - Parse docker-compose.yml using compose-go library
   - Build or pull all referenced images (preferring ARM64)
   - Save images as tar files for embedding
   - Generate modified compose file with embedded image references
3. **Binary Building** (`internal/nix/build.go`): If autodiscovery enabled, cross-compile Sprout binary for ARM64/Linux to embed in image
4. **Template Generation** (`internal/nix/image.go`): Populate embedded Nix template (`image.nix.tmpl`) with configuration, Docker images, and binary
5. **Image Building**:
   - **Local mode** (`internal/nix/build_local.go`): Use system `nix-build` directly
   - **Docker mode** (`internal/nix/build_docker.go`): Run `nix-build` inside nixos/nix container, mounting necessary files
6. **Image Copying** (`internal/cmd/seed.go`): Copy built image from Nix store to user-specified path

### Key Components

**CLI Layer** (`internal/cmd/`):
- `seed.go`: Orchestrates entire image build process with progress output
- `burn.go`: Interactive SD card flashing with safety checks, platform-specific disk detection
- `discover.go`: Client for finding Sprout devices via mDNS
- `daemon.go`: Server that broadcasts mDNS presence

**Nix Integration** (`internal/nix/`):
- `config.go`: YAML loading and validation
- `docker.go`: Docker Compose parsing, image building/pulling, tar export
- `build.go`: Smart selection between local Nix or Docker build
- `build_local.go` / `build_docker.go`: Platform-specific build implementations
- `types.go`: Core data structures (SproutFile, DockerComposeConfig, DockerImage)

**Discovery** (`internal/discovery/`):
- mDNS-based service discovery using `_sprout._tcp` service name
- Automatic network interface selection for reliable discovery
- Server broadcasts hostname and IP for `sprout discover` command

**Disk Operations** (`internal/burn/`):
- Platform-specific removable disk detection (macOS: `diskutil`, Linux: `lsblk`)
- Safe disk selection with protection against system disk overwrite
- Progress tracking for dd operations

### Build Mode Selection

Set `SPROUT_DISABLE_LOCAL_NIX=1` to force Docker builds even when Nix is available. Otherwise:
- If `nix-build` exists in PATH → use local Nix (faster, uses local cache)
- If `nix-build` not found → use Docker with nixos/nix image

### Key Dependencies

- `github.com/spf13/cobra`: CLI framework
- `gopkg.in/yaml.v3`: YAML parsing
- `github.com/compose-spec/compose-go/v2`: Docker Compose file parsing
- `github.com/docker/docker`: Docker client for image operations
- `github.com/hashicorp/mdns`: mDNS discovery protocol
- `text/template`: Nix template rendering