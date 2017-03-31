package main

import (
	"github.com/ssut/pocketnpm/db"
	"github.com/ssut/pocketnpm/npm"
)

type PocketConfig struct {
	DB     db.DatabaseConfig `toml:"database"`
	Mirror npm.MirrorConfig  `toml:"mirror"`
	Server ServerConfig      `toml:"server"`
}

type ServerConfig struct {
	Bind         string `toml:"bind"`
	Host         string `toml:"host"`
	Port         int    `toml:"port"`
	EnableXAccel bool   `toml:"x_accel_redirect"`
}
