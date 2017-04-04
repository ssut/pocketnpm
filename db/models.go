package db

import "net/url"

const (
	MarkIncomplete = "0"
	MarkComplete   = "1"
)

// DatabaseConfig defines config for database
type DatabaseConfig struct {
	Type          string `toml:"type"`
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

// PocketStore represents the database factory interface
type PocketStore interface {
	Connect()
	Init()
	IsInitialized() bool
	GetItemCount(string) int
	GetStats() *DatabaseStats
	GetSequence() int
	SetSequence(int)
	GetCountOfMarks(bool) int
	GetIncompletePackages() []*BarePackage
	GetRevision(string) string
	GetDocument(string, bool) (string, []*url.URL, error)
	GetAllFiles() map[string][]*url.URL
	PutPackage(string, string, bool, bool) error
	PutPackages([]*BarePackage)
	DeletePackage(string)
	PutCompleted(*BarePackage, string, string, []*url.URL) bool
}

// StoreType represents the type for database store
type StoreType int

const (
	// BoltStore uses boltdb as a database backend
	BoltStore StoreType = 1 << iota
	// GormStore uses gorm as a database backend controller
	GormStore
)
