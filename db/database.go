package db

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"encoding/binary"

	"net/url"

	"bytes"
	"encoding/gob"

	"github.com/Sirupsen/logrus"
	"github.com/allegro/bigcache"
	"github.com/boltdb/bolt"
	pbar "gopkg.in/cheggaaa/pb.v1"

	"github.com/ssut/pocketnpm/log"
)

// PocketBase type is a frontend for BoltDB
type PocketBase struct {
	db    *bolt.DB
	cache *bigcache.BigCache
	path  string
}

// NewPocketBase creates a new PocketBase object
func NewPocketBase(config *DatabaseConfig, readonly bool) *PocketBase {
	path, _ := filepath.Abs(config.Path)
	dbConfig := &bolt.Options{}
	if readonly {
		dbConfig.ReadOnly = true
	}

	db, err := bolt.Open(path, 0600, dbConfig)
	if err != nil {
		log.Fatalf("Failed to load database file: %v", err)
	}

	cacheConfig := bigcache.DefaultConfig(time.Duration(config.CacheLifetime) * time.Minute)
	cacheConfig.MaxEntrySize = 8192
	cacheConfig.HardMaxCacheSize = config.MaxCacheSize
	cache, err := bigcache.NewBigCache(cacheConfig)
	if err != nil {
		log.Fatalf("Failed to initialize in-memory cache: %v", err)
	}

	gob.Register([]*url.URL{})

	pb := &PocketBase{
		db:    db,
		cache: cache,
		path:  path,
	}

	return pb
}

// Close method closes database connection
func (pb *PocketBase) Close() {
	pb.db.Close()
}

// LogStats method logs database stats every 10 seconds
func (pb *PocketBase) LogStats() {
	prev := pb.db.Stats()

	for {
		time.Sleep(10 * time.Second)

		stats := pb.db.Stats()
		cached := pb.cache.Len()
		diff := stats.Sub(&prev)

		log.WithFields(logrus.Fields{
			"memcached":    cached,
			"openTxN":      diff.OpenTxN,
			"pendingPageN": stats.PendingPageN,
		}).Info("DB Status")

		prev = stats
	}
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

// IsInitialized method returns whether the database has initialized
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

// Check method checks redundancy for the buckets
func (pb *PocketBase) Check() {
	tx, err := pb.db.Begin(false)
	if err != nil {
		log.Panic(err)
	}
	defer tx.Commit()

	packages := tx.Bucket([]byte("Packages"))
	documents := tx.Bucket([]byte("Documents"))
	files := tx.Bucket([]byte("Files"))
	marks := tx.Bucket([]byte("Marks"))

	// 1. get all marked items
	var availables [][]byte
	marksCursor := marks.Cursor()

	for k, v := marksCursor.First(); k != nil; k, v = marksCursor.Next() {
		if string(v) == MarkComplete {
			availables = append(availables, k)
		}
	}

	var inconsistencies []string

	// 2. check marked items have a revision, a document, and a file
	count := len(availables)
	log.Infof("Checking consistency for %d items", count)
	bar := pbar.StartNew(count)
	for _, key := range availables {
		if rev := packages.Get(key); rev == nil || len(rev) == 0 {
			inconsistencies = append(inconsistencies, string(key)+" (rev)")
		} else if doc := documents.Get(key); doc == nil || len(doc) == 0 {
			inconsistencies = append(inconsistencies, string(key)+" (doc)")
		} else if file := files.Get(key); file == nil {
			inconsistencies = append(inconsistencies, string(key)+" (file)")
		}
		bar.Increment()
	}
	bar.Finish()

	// 3. print errors
	log.Infof("%d errors found in database", len(inconsistencies))
	log.Error(strings.Join(inconsistencies[:], ", "))
}

func (pb *PocketBase) getCacheDecoder(key string) *gob.Decoder {
	cache, err := pb.cache.Get(key)
	if err != nil {
		return nil
	}
	if len(cache) == 0 {
		return nil
	}

	var buf bytes.Buffer
	buf.Write(cache)
	dec := gob.NewDecoder(&buf)
	return dec
}

func (pb *PocketBase) setCache(key string, value interface{}) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(value)
	if err != nil {
		log.Debug(err)
		return
	}

	pb.cache.Set(key, buf.Bytes())
}

func (pb *PocketBase) delCache(key string) {
	pb.cache.Set(key, []byte{})
}

// GetItemCount method returns the count of items in the bucket
func (pb *PocketBase) GetItemCount(name string) int {
	var count int

	dec := pb.getCacheDecoder("count:" + name)
	if dec != nil {
		var cache int
		decerr := dec.Decode(&cache)
		if decerr == nil {
			count = cache
			return count
		}
	}

	pb.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(name))
		c := b.Cursor()

		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			count++
		}

		return nil
	})

	pb.setCache("count:"+name, count)
	return count
}

// GetStats method returns status of various buckets
func (pb *PocketBase) GetStats() *DatabaseStats {
	stats := &DatabaseStats{
		Packages:  pb.GetItemCount("Packages"),
		Marks:     pb.GetItemCount("Marks"),
		Documents: pb.GetItemCount("Documents"),
		Files:     pb.GetItemCount("Files"),
	}

	return stats
}

// GetSequence method returns current sequence of registry
func (pb *PocketBase) GetSequence() int {
	var sequence int

	dec := pb.getCacheDecoder("global:sequence")
	if dec != nil {
		var cache int
		decerr := dec.Decode(&cache)
		if decerr == nil {
			sequence = cache
			return sequence
		}
	}

	pb.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("Globals"))
		v := b.Get([]byte("sequence"))
		sequence = int(binary.LittleEndian.Uint32(v))
		return nil
	})

	pb.setCache("global:sequence", sequence)

	return sequence
}

// SetSequence method sets current sequence of registry
func (pb *PocketBase) SetSequence(seq int) {
	pb.db.Update(func(tx *bolt.Tx) error {
		global := tx.Bucket([]byte("Globals"))

		byteSequence := make([]byte, 4)
		binary.LittleEndian.PutUint32(byteSequence, uint32(seq))
		global.Put([]byte("sequence"), byteSequence)

		return nil
	})

	pb.setCache("global:sequence", seq)
}

// GetCountOfMarks method returns a count of marked items
func (pb *PocketBase) GetCountOfMarks(cond bool) int {
	condition := MarkIncomplete
	if cond {
		condition = MarkComplete
	}

	var count int

	dec := pb.getCacheDecoder("mark:" + condition)
	if dec != nil {
		var cache interface{}
		decerr := dec.Decode(&cache)
		if decerr == nil {
			count = cache.(int)
			return count
		}
	}

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

	pb.setCache("mark:"+condition, count)
	return count
}

// GetIncompletePackages method returns a list of packages that are ready for queueing
func (pb *PocketBase) GetIncompletePackages() []*BarePackage {
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

// GetRevision method returns a revision of document
func (pb *PocketBase) GetRevision(id string) (rev string) {
	rev = ""
	key := []byte(id)

	dec := pb.getCacheDecoder(id + ":rev")
	if dec != nil {
		var cache interface{}
		decerr := dec.Decode(&cache)
		if decerr == nil {
			rev = cache.(string)
			return
		}
	}

	pb.db.View(func(tx *bolt.Tx) error {
		packages := tx.Bucket([]byte("Packages"))
		val := packages.Get(key)
		if val != nil {
			rev = string(val)
		}

		return nil
	})

	if rev != "" {
		pb.setCache(id+":rev", rev)
	}
	return
}

// GetDocument method returns a document by given name
func (pb *PocketBase) GetDocument(id string, withfiles bool) (document string, filelist []*url.URL, err error) {
	document = "{}"
	filelist = nil

	key := []byte(id)

	dec := pb.getCacheDecoder(id)
	if dec != nil {
		var caches []interface{}
		decerr := dec.Decode(&caches)
		if decerr == nil {
			document = caches[0].(string)
			filelist = caches[1].([]*url.URL)
			return
		}
	}

	pb.db.View(func(tx *bolt.Tx) error {
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
			var buf bytes.Buffer
			buf.Write(files.Get(key))
			dec := gob.NewDecoder(&buf)
			decerr := dec.Decode(&filelist)
			if decerr != nil {
				err = fmt.Errorf("Internal error: %v", decerr)
				return nil
			}
		}

		return nil
	})

	if document != "" && document != "{}" {
		caches := []interface{}{
			document,
			filelist,
		}
		pb.setCache(id, caches)
	}

	return
}

// GetAllFiles method returns {name: files} map
func (pb *PocketBase) GetAllFiles() (all map[string][]*url.URL) {
	all = map[string][]*url.URL{}

	pb.db.View(func(tx *bolt.Tx) error {
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

// PutPackage method inserts a package into the appropriate buckets
func (pb *PocketBase) PutPackage(tx *bolt.Tx, id string, rev string, mark bool, overwrite bool) error {
	defer pb.delCache("count:Packages")
	defer pb.delCache("count:Marks")
	defer pb.delCache("mark:0")
	defer pb.delCache("mark:1")

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

// PutPackages method is a bulk method of PutPackage
func (pb *PocketBase) PutPackages(allDocs []*BarePackage) {
	tx, _ := pb.db.Begin(true)
	defer tx.Rollback()

	for _, doc := range allDocs {
		err := pb.PutPackage(tx, doc.ID, doc.Revision, false, true)
		if err != nil {
			log.Error(err)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Fatal(err)
	}
}

// DeletePackage method deletes a package
func (pb *PocketBase) DeletePackage(name string) {
	key := []byte(name)

	defer pb.delCache("count:Packages")
	defer pb.delCache("count:Documents")
	defer pb.delCache("count:Files")
	defer pb.delCache("count:Marks")
	defer pb.delCache("mark:0")
	defer pb.delCache("mark:1")

	pb.db.Update(func(tx *bolt.Tx) error {
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

// PutCompleted method inserts a completed package into the appropriate buckets
func (pb *PocketBase) PutCompleted(pack *BarePackage, document string, rev string, downloads []*url.URL) bool {
	tx, _ := pb.db.Begin(true)
	defer tx.Rollback()
	defer pb.delCache(document)
	defer pb.delCache(document + ":rev")
	defer pb.delCache("count:Packages")
	defer pb.delCache("count:Documents")
	defer pb.delCache("count:Files")
	defer pb.delCache("count:Marks")

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
