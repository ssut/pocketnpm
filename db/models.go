package db

// DatabaseConfig defines config for database
type DatabaseConfig struct {
	Driver string `toml:"driver"`
	Source string `toml:"source"`
}

// DatabaseStats represents the count of each bucket
type DatabaseStats struct {
	Packages  int
	Marks     int
	Documents int
	Files     int
}

// Package represents the basis elements for package
type Package struct {
	ID       string
	Revision string
}

type transactionable interface {
	Commit() error
	Rollback() error
}
