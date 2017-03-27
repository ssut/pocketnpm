package main

type PocketConfig struct {
	DB     databaseConfig `toml:"database"`
	Mirror mirrorConfig   `toml:"mirror"`
	Server serverConfig   `toml:"server"`
}

type databaseConfig struct {
	Path string `toml:"path"`
}

type mirrorConfig struct {
	Registry       string `toml:"registry"`
	MaxConnections int    `toml:"max_connections"`
	Path           string `toml:"path"`
}

type serverConfig struct {
	Host string `toml:"host"`
	Port int    `toml:"port"`
}
