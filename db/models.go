package db

// DatabaseConfig defines config for database
type DatabaseConfig struct {
	Driver string `toml:"driver"`
	Source string `toml:"source"`
}

// DatabaseStats represents the count of each bucket
type DatabaseStats struct {
	Packages  int64
	Completed int64
	Files     int64
}

// Package represents the basis elements for package
type Package struct {
	ID       []byte
	Revision string
	Dists    []*Dist
}

type Dist struct {
	Hash       string
	URL        string
	Downloaded bool
}

func (p *Package) IDString() string {
	return string(p.ID)
}

func NewPackage(id string, revision string) *Package {
	return &Package{
		ID:       []byte(id),
		Revision: revision,
	}
}

type transactionable interface {
	Commit() error
	Rollback() error
}
