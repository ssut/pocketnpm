package db

import "net/url"

const (
	MarkIncomplete = "0"
	MarkComplete   = "1"
)

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

// BarePackage represents the basis elements for package
type BarePackage struct {
	ID       string
	Revision string
}

type transactionable interface {
	Commit() error
	Rollback() error
}

// PocketStore represents the database factory interface
type PocketStore interface {
	Connect() error
	Close()
	Init()
	IsInitialized() bool
	GetItemCount(string) int
	GetSequence() int
	SetSequence(int)
	GetCountOfMarks(string) int
	GetIncompletePackages() []*BarePackage
	GetRevision(string) string
	GetDocument(string, bool) (string, []byte, error)
	GetAllFiles() map[string][]*url.URL
	AcquireTx() transactionable
	PutPackage(transactionable, string, string, bool, bool) error
	DeletePackage(string)
	PutCompleted(transactionable, *BarePackage, string, string, []*url.URL) bool
}
