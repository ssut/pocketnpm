package db

// DatabaseConfig defines config for database
type DatabaseConfig struct {
	Path string `toml:"path"`
}

// DatabaseStats represents the count of each bucket
type DatabaseStats struct {
	Packages  int
	Marks     int
	Documents int
	Files     int
}

// BarePackage represents the basis elements for package
type BarePackage struct {
	ID       string
	Revision string
}
