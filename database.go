package main

import (
	"path/filepath"

	"github.com/boltdb/bolt"
)

// PocketBase type is a frontend for BoltDB
type PocketBase struct {
	path string
}

func NewPocketBase(config *databaseConfig) *PocketBase {
	path, _ := filepath.Abs(config.Path)
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		log.Fatalf("Failed to load database file: %s", err)
	}
	defer db.Close()

	pb := &PocketBase{
		path: path,
	}

	return pb
}

func (pb *PocketBase) Open() *bolt.DB {
	db, err := bolt.Open(pb.path, 0600, nil)
	if err != nil {
		log.Fatalf("Failed to load database file: %s", err)
	}

	return db
}
