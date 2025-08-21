package sprout

type Config struct {
	Server []ServerConfig `toml:"server"`
}

type ServerConfig struct {
	Name          string        `toml:"name"`
	Host          string        `toml:"host"`
	Port          int           `toml:"port"`
	NetworkConfig NetworkConfig `toml:"network"`
}

type NetworkConfig struct {
	NetworkType string `toml:"type"`
	NetworkName string `toml:"name"`
}
