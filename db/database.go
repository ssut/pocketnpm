package db

import (
	"path/filepath"

	"encoding/binary"

	"github.com/boltdb/bolt"

	"net/url"

	"bytes"
	"encoding/gob"

	"github.com/ssut/pocketnpm/log"
)

// PocketBase type is a frontend for BoltDB
type PocketBase struct {
	db   *bolt.DB
	path string
}

// NewPocketBase creates a new PocketBase object
func NewPocketBase(config *DatabaseConfig) *PocketBase {
	path, _ := filepath.Abs(config.Path)
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		log.Fatalf("Failed to load database file: %s", err)
	}

	pb := &PocketBase{
		db:   db,
		path: path,
	}

	return pb
}

// Close method closes database connection
func (pb *PocketBase) Close() {
	pb.db.Close()
}

// Init method initializes scheme
//
// Globals bucket contains global variables such as "sequence"
// Packages bucket contains the map of "id": "revision" hash
// Marks bucket contains the state to determine whether package currently downloaded or not
// Documents bucket contains full document of the package
// Files bucket con
func (pb *PocketBase) Init() {
	pb.db.Update(func(tx *bolt.Tx) error {
		global, _ := tx.CreateBucketIfNotExists([]byte("Globals"))
		tx.CreateBucketIfNotExists([]byte("Packages"))
		tx.CreateBucketIfNotExists([]byte("Marks"))
		tx.CreateBucketIfNotExists([]byte("Documents"))
		tx.CreateBucketIfNotExists([]byte("Files"))

		defaultSequence := make([]byte, 4)
		binary.LittleEndian.PutUint32(defaultSequence, 0)
		global.Put([]byte("sequence"), defaultSequence)
		return nil
	})
}

func (pb *PocketBase) IsInitialized() bool {
	initialized := false
	pb.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("Globals"))
		if b != nil {
			v := b.Get([]byte("sequence"))
			initialized = v != nil
		}
		return nil
	})

	return initialized
}

func (pb *PocketBase) GetItemCount(name string) int {
	var count int
	pb.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(name))

		b.ForEach(func(k, v []byte) error {
			count++
			return nil
		})

		return nil
	})

	return count
}

func (pb *PocketBase) GetStats() *DatabaseStats {
	stats := &DatabaseStats{
		Packages:  pb.GetItemCount("Packages"),
		Marks:     pb.GetItemCount("Marks"),
		Documents: pb.GetItemCount("Documents"),
		Files:     pb.GetItemCount("Files"),
	}

	return stats
}

func (pb *PocketBase) GetSequence() int {
	sequence := 0
	pb.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("Globals"))
		v := b.Get([]byte("sequence"))
		sequence = int(binary.LittleEndian.Uint32(v))
		return nil
	})

	return sequence
}

func (pb *PocketBase) SetSequence(seq int) {
	pb.db.Update(func(tx *bolt.Tx) error {
		global := tx.Bucket([]byte("Globals"))

		byteSequence := make([]byte, 4)
		binary.LittleEndian.PutUint32(byteSequence, uint32(seq))
		global.Put([]byte("sequence"), byteSequence)

		return nil
	})
}

func (pb *PocketBase) GetCountOfMarks(cond bool) int {
	condition := MarkIncomplete
	if cond {
		condition = MarkComplete
	}
	count := 0

	pb.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("Marks"))
		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			if string(v) == condition {
				count++
			}
		}
		return nil
	})

	return count
}

func (pb *PocketBase) GetImcompletePackages() []*BarePackage {
	count := pb.GetCountOfMarks(false)
	packages := make([]*BarePackage, count)

	pb.db.View(func(tx *bolt.Tx) error {
		packs := tx.Bucket([]byte("Packages"))
		marks := tx.Bucket([]byte("Marks"))

		i := 0
		c := marks.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if string(v) == MarkIncomplete {
				revision := packs.Get(k)
				packages[i] = &BarePackage{
					ID:       string(k),
					Revision: string(revision),
				}
				i++
			}
		}

		return nil
	})

	return packages
}

func (pb *PocketBase) PutPackage(tx *bolt.Tx, id string, rev string, mark bool) error {
	packages := tx.Bucket([]byte("Packages"))
	marks := tx.Bucket([]byte("Marks"))
	key := []byte(id)

	// Check if package's already exists
	if value := packages.Get(key); value != nil {
		return nil
	}

	err := packages.Put(key, []byte(rev))
	if err != nil {
		return err
	}

	marked := []byte(MarkIncomplete)
	if mark {
		marked = []byte(MarkComplete)
	}
	err = marks.Put(key, marked)
	if err != nil {
		return err
	}

	return nil
}

func (pb *PocketBase) PutPackages(allDocs []*BarePackage) {
	tx, _ := pb.db.Begin(true)
	defer tx.Rollback()

	for _, doc := range allDocs {
		err := pb.PutPackage(tx, doc.ID, doc.Revision, false)
		if err != nil {
			log.Error(err)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Fatal(err)
	}
}

func (pb *PocketBase) PutCompleted(pack *BarePackage, document string, rev string, downloads []*url.URL) bool {
	tx, _ := pb.db.Begin(true)
	defer tx.Rollback()

	key := []byte(pack.ID)

	packages := tx.Bucket([]byte("Packages"))
	documents := tx.Bucket([]byte("Documents"))
	files := tx.Bucket([]byte("Files"))
	marks := tx.Bucket([]byte("Marks"))

	// if revision does not match
	if pack.Revision != rev {
		err := packages.Put(key, []byte(rev))
		if err != nil {
			return false
		}
	}

	err := documents.Put(key, []byte(document))
	if err != nil {
		return false
	}

	// encode downloads(interface) directly into a byte array
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err = enc.Encode(downloads)
	if err != nil {
		return false
	}

	err = files.Put(key, buf.Bytes())
	if err != nil {
		return false
	}

	err = marks.Put(key, []byte(MarkComplete))
	if err != nil {
		return false
	}

	if err = tx.Commit(); err != nil {
		return false
	}

	return true
}
