package nix

type Nix struct{}

type NetworkConfig struct {
	PSK string `yaml:"psk"`
}

type WirelessConfig struct {
	Enabled  bool                     `yaml:"enabled"`
	Networks map[string]NetworkConfig `yaml:"networks"`
}

type OutputConfig struct {
	Path string `yaml:"path"`
}

type DockerImage struct {
	Name     string
	LocalTag string
	TarPath  string
}

type DockerComposeConfig struct {
	Enabled         bool          `yaml:"enabled"`
	Path            string        `yaml:"path"`
	Content         string
	ModifiedContent string
	Images          []DockerImage
}

type SproutFile struct {
	SSHKeys          []string            `yaml:"ssh_keys"`
	Username         string              `yaml:"username"`
	Wireless         WirelessConfig      `yaml:"wireless"`
	Output           OutputConfig        `yaml:"output"`
	DockerCompose    DockerComposeConfig `yaml:"docker_compose"`
	Autodiscovery    bool                `yaml:"autodiscovery"`
	SproutBinaryPath string
}
