package main

import (
	"github.com/ssut/pocketnpm/db"
	"github.com/ssut/pocketnpm/npm"
)

type PocketConfig struct {
	DB     db.DatabaseConfig `toml:"database"`
	Mirror npm.MirrorConfig  `toml:"mirror"`
	Server npm.ServerConfig  `toml:"server"`
}
