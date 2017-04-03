package db

const (
	MarkIncomplete = "0"
	MarkComplete   = "1"
)

// DatabaseConfig defines config for database
type DatabaseConfig struct {
	Path          string `toml:"path"`
	MaxCacheSize  int    `toml:"max_cache_size"`
	CacheLifetime int    `toml:"cache_lifetime"`
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
