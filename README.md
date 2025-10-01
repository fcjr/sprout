# Sprout 🌱

**Turn your `docker-compose.yml` into bootable NixOS images.**

> ⚠️ **Pre-Alpha Software**: Sprout is currently an experiment. The API and architecture will likely change significantly. Use at your own risk.

## Why Sprout?

Want to run Docker Compose on a Raspberry Pi or other embedded system? The traditional approach is tedious:

1. Flash a base OS (Raspbian, Ubuntu, etc.)
2. Boot and connect a monitor/keyboard to configure WiFi
3. SSH in and run endless updates
4. Install Docker and Docker Compose
5. Clone your repo and configure everything
6. Set up SSH keys, networking, auto-start scripts...
7. Repeat for every device

**Sprout does all of this in one command.** Write a `docker-compose.yml` and a simple config file, run `sprout seed`, and get a ready-to-boot SD card image with everything configured.

**With Sprout:**
```yaml
# sprout.yaml - everything in one place
ssh_keys:
  - ssh-ed25519 AAAAC3...

wireless:
  enabled: true
  networks:
    - ssid: "MyWiFi"
      psk: "password"

docker_compose:
  enabled: true
  file: docker-compose.yml
```

```bash
sprout seed  # Generates bootable image with everything configured
sprout burn build/image.img  # Flash to SD card
# Done! Just boot the Pi and everything works
```

## Features

- **Docker Compose First**: Embed your entire Docker Compose stack in a bootable image
- **Zero Configuration**: Automatic SSH key setup, wireless networking, and discovery
- **Cross-Platform Building**: Build ARM images on macOS/Linux with or without Nix installed
- **Auto-Discovery**: Built-in mDNS/Bonjour for finding devices on your network
- **SD Card Burning**: Integrated tool to flash images directly to SD cards

## Quick Start

### 1. Install Sprout

```bash
# Download the latest release
# (Installation instructions coming soon)
```

### 2. Create a sprout.yaml

**With Docker Compose** (recommended):
```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/fcjr/sprout/main/sprout.schema.json

ssh_keys:
  - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample...

docker_compose:
  enabled: true
  file: docker-compose.yml

autodiscovery: true

output:
  path: build/image.img
```

> **💡 Tip:** The schema comment enables autocomplete and validation in editors like VS Code with the YAML extension.

**With just SSH and WiFi**:
```yaml
ssh_keys:
  - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample...

wireless:
  enabled: true
  networks:
    - ssid: "MyWiFi"
      psk: "password123"

autodiscovery: true

output:
  path: build/image.img
```

### 3. Build the Image

```bash
sprout seed
```

This generates a bootable NixOS image at `build/image.img` with:
- SSH access configured
- Your Docker Compose stack ready to run (if enabled)
- WiFi configured (if enabled)
- mDNS autodiscovery enabled

### 4. Flash to SD Card

```bash
sprout burn build/image.img
```

### 5. Boot and Discover

```bash
sprout discover
```

Find your device on the network, then SSH in:
```bash
ssh sprout@<discovered-ip>
```

## Configuration Reference

### SSH Keys
```yaml
ssh_keys:
  - ssh-ed25519 AAAAC3...
  - ssh-rsa AAAAB3...
```

Add your public SSH keys to enable remote access.

### Wireless Networks
```yaml
wireless:
  enabled: true
  networks:
    - ssid: "HomeNetwork"
      psk: "password123"
    - ssid: "WorkNetwork"
      psk: "different-password"
```

### Docker Compose
```yaml
docker_compose:
  enabled: true
  file: docker-compose.yml  # Path relative to sprout.yaml
```

Embeds your entire Docker Compose stack into the image. All services start automatically on boot.

### Auto-Discovery
```yaml
autodiscovery: true
```

Enables mDNS/Bonjour broadcasting so you can find your device with `sprout discover`.

### Output Path
```yaml
output:
  path: build/image.img  # Can be absolute or relative
```

## Architecture

Sprout is a Go CLI tool that:

1. Parses your `sprout.yaml` configuration
2. Processes your Docker Compose file (if enabled)
3. Generates a NixOS configuration from templates
4. Builds an ARM64 bootable image using either:
   - Local `nix-build` (if Nix is installed)
   - Docker container with Nix (if Nix isn't available)
5. Copies the result to your specified output path

The generated images are standard NixOS SD card images tailored for ARM64 systems like Raspberry Pi.

## Commands

- `sprout seed` - Generate a bootable image from sprout.yaml
- `sprout burn <image>` - Flash an image to an SD card
- `sprout discover` - Find Sprout devices on your network
- `sprout daemon` - Run the discovery daemon (advanced)

## Current Limitations

- **ARM64 only**: Currently generates Raspberry Pi compatible images
- **Linux/macOS only**: Windows support not yet implemented
- **Experimental**: APIs and configuration format may change
- **Limited testing**: Use in production at your own risk

## Requirements

**For building images:**
- Go 1.21+
- Either:
  - Nix with flakes enabled, OR
  - Docker (Sprout will use nixos/nix container)

**For burning images:**
- macOS: `diskutil` (built-in)
- Linux: `lsblk`, `dd` (usually pre-installed)

## Development

```bash
# Build
go build -o sprout cmd/sprout/main.go

# Run
./sprout seed

# Test
go test ./...
```

See [CLAUDE.md](CLAUDE.md) for detailed development information.

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Contributing

This is pre-alpha software. Contributions, ideas, and feedback are welcome, but please note that the project is likely to undergo significant changes.

---

Made with ❤️ at the [Recurse Center](https://www.recurse.com/scout/click?t=ba46ea16fafed13b3f8ccacb0ce83ad1)
