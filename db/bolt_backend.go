package db

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"log"
	"net/url"

	"github.com/boltdb/bolt"
)

type boltStore struct {
	db     *bolt.DB
	config *DatabaseConfig
}

func newBoltStore(config *DatabaseConfig) PocketStore {
	store := &boltStore{
		config: config,
	}

	return store
}

func (store *boltStore) Connect() error {
	var err error
	store.db, err = bolt.Open(store.config.Path.(string), 0600, nil)
	if err != nil {
		log.Fatalf("Failed to load database file: %v", err)
		return err
	}

	return nil
}

func (store *boltStore) Close() {
	store.db.Close()
}

func (store *boltStore) Init() {
	store.db.Update(func(tx *bolt.Tx) error {
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

func (store *boltStore) IsInitialized() bool {
	initialized := false
	store.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("Globals"))
		if b != nil {
			v := b.Get([]byte("sequence"))
			initialized = v != nil
		}
		return nil
	})

	return initialized
}

func (store *boltStore) GetItemCount(name string) (count int) {
	store.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(name))
		c := b.Cursor()

		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			count++
		}

		return nil
	})

	return
}

func (store *boltStore) GetSequence() (sequence int) {
	store.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("Globals"))
		v := b.Get([]byte("sequence"))
		sequence = int(binary.LittleEndian.Uint32(v))
		return nil
	})

	return
}

func (store *boltStore) SetSequence(seq int) {
	store.db.Update(func(tx *bolt.Tx) error {
		global := tx.Bucket([]byte("Globals"))

		byteSequence := make([]byte, 4)
		binary.LittleEndian.PutUint32(byteSequence, uint32(seq))
		global.Put([]byte("sequence"), byteSequence)

		return nil
	})
}

func (store *boltStore) GetCountOfMarks(cond string) (count int) {
	store.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("Marks"))
		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			if string(v) == cond {
				count++
			}
		}
		return nil
	})

	return
}

func (store *boltStore) GetIncompletePackages() (packages []*BarePackage) {
	store.db.View(func(tx *bolt.Tx) error {
		packs := tx.Bucket([]byte("Packages"))
		marks := tx.Bucket([]byte("Marks"))

		c := marks.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if string(v) == MarkIncomplete {
				revision := packs.Get(k)
				pack := &BarePackage{
					ID:       string(k),
					Revision: string(revision),
				}

				packages = append(packages, pack)
			}
		}

		return nil
	})

	return
}

func (store *boltStore) GetRevision(id string) (rev string) {
	key := []byte(id)
	store.db.View(func(tx *bolt.Tx) error {
		packages := tx.Bucket([]byte("Packages"))
		val := packages.Get(key)
		if val != nil {
			rev = string(val)
		}

		return nil
	})

	return
}

func (store *boltStore) GetDocument(id string, withfiles bool) (document string, rawfiles []byte, err error) {
	key := []byte(id)

	store.db.View(func(tx *bolt.Tx) error {
		documents := tx.Bucket([]byte("Documents"))
		marks := tx.Bucket([]byte("Marks"))
		files := tx.Bucket([]byte("Files"))

		mark := marks.Get(key)
		if mark == nil {
			err = errors.New("Package does not exist")
			return nil
		}

		documentBytes := documents.Get(key)

		if (documentBytes == nil || string(documentBytes) == "") && string(mark) == MarkIncomplete {
			err = errors.New("Package has not been downloaded yet")
			return nil
		}

		document = string(documentBytes)

		if withfiles {
			rawfiles = files.Get(key)
		}

		return nil
	})

	return
}

func (store *boltStore) GetAllFiles() (all map[string][]*url.URL) {
	store.db.View(func(tx *bolt.Tx) error {
		files := tx.Bucket([]byte("Files"))
		cursor := files.Cursor()

		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var filelist []*url.URL
			var buf bytes.Buffer
			buf.Write(v)
			dec := gob.NewDecoder(&buf)
			dec.Decode(&filelist)

			all[string(k)] = filelist
		}

		return nil
	})

	return
}

func (store *boltStore) AcquireTx() transactionable {
	tx, _ := store.db.Begin(true)
	return tx
}

func (store *boltStore) PutPackage(tr transactionable, id string, rev string, mark bool, overwrite bool) error {
	tx := tr.(*bolt.Tx)
	packages := tx.Bucket([]byte("Packages"))
	marks := tx.Bucket([]byte("Marks"))
	key := []byte(id)

	// Check if package's already exists
	if value := packages.Get(key); value != nil && !overwrite {
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

func (store *boltStore) DeletePackage(id string) {
	key := []byte(id)

	store.db.Update(func(tx *bolt.Tx) error {
		packages := tx.Bucket([]byte("Packages"))
		documents := tx.Bucket([]byte("Documents"))
		files := tx.Bucket([]byte("Files"))
		marks := tx.Bucket([]byte("Marks"))

		packages.Delete(key)
		documents.Delete(key)
		files.Delete(key)
		marks.Delete(key)

		return nil
	})
}

func (store *boltStore) PutCompleted(tr transactionable, pack *BarePackage, document string, rev string, downloads []*url.URL) bool {
	tx := tr.(*bolt.Tx)
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

	return true
}
