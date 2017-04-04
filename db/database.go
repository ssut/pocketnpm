package db

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

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
	store PocketStore
	cache *bigcache.BigCache
}

// NewPocketBase creates a new PocketBase object
func NewPocketBase(config *DatabaseConfig) *PocketBase {
	var store PocketStore
	if config.Type == "bolt" {
		config.Path, _ = filepath.Abs(config.Path.(string))
		store = newBoltStore(config)
	} else if config.Type == "gorm" {
		store = newGormStore(config)
	}
	err := store.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
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
		store: store,
		cache: cache,
	}

	return pb
}

// Close method closes database connection
func (pb *PocketBase) Close() {
	pb.store.Close()
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
	pb.store.Init()
}

// IsInitialized method returns whether the database has initialized
func (pb *PocketBase) IsInitialized() bool {
	return pb.store.IsInitialized()
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
func (pb *PocketBase) GetItemCount(name string) (count int) {
	dec := pb.getCacheDecoder("count:" + name)
	if dec != nil {
		var cache int
		decerr := dec.Decode(&cache)
		if decerr == nil {
			count = cache
			return count
		}
	}

	count = pb.store.GetItemCount(name)

	pb.setCache("count:"+name, count)
	return
}

// GetStats method returns status of various buckets
func (pb *PocketBase) GetStats() (stats *DatabaseStats) {
	stats = &DatabaseStats{
		Packages:  pb.GetItemCount("Packages"),
		Marks:     pb.GetItemCount("Marks"),
		Documents: pb.GetItemCount("Documents"),
		Files:     pb.GetItemCount("Files"),
	}

	return
}

// GetSequence method returns current sequence of registry
func (pb *PocketBase) GetSequence() (sequence int) {
	dec := pb.getCacheDecoder("global:sequence")
	if dec != nil {
		var cache int
		decerr := dec.Decode(&cache)
		if decerr == nil {
			sequence = cache
			return sequence
		}
	}

	sequence = pb.store.GetSequence()

	pb.setCache("global:sequence", sequence)

	return
}

// SetSequence method sets current sequence of registry
func (pb *PocketBase) SetSequence(seq int) {
	pb.store.SetSequence(seq)

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

	count = pb.store.GetCountOfMarks(condition)

	pb.setCache("mark:"+condition, count)
	return count
}

// GetIncompletePackages method returns a list of packages that are ready for queueing
func (pb *PocketBase) GetIncompletePackages() []*BarePackage {
	return pb.store.GetIncompletePackages()
}

// GetRevision method returns a revision of document
func (pb *PocketBase) GetRevision(id string) (rev string) {
	dec := pb.getCacheDecoder(id + ":rev")
	if dec != nil {
		var cache interface{}
		decerr := dec.Decode(&cache)
		if decerr == nil {
			rev = cache.(string)
			return
		}
	}

	rev = pb.store.GetRevision(id)

	if rev != "" {
		pb.setCache(id+":rev", rev)
	}
	return
}

// GetDocument method returns a document by given name
func (pb *PocketBase) GetDocument(id string, withfiles bool) (document string, filelist []*url.URL, err error) {
	document = "{}"
	filelist = nil

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

	document, rawfiles, err := pb.store.GetDocument(id, withfiles)
	if err != nil {
		return
	}

	if withfiles {
		var buf bytes.Buffer
		buf.Write(rawfiles)
		dec = gob.NewDecoder(&buf)
		decerr := dec.Decode(&filelist)
		if decerr != nil {
			err = fmt.Errorf("Internal error: %v", decerr)
			return "", nil, nil
		}
	}

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
func (pb *PocketBase) GetAllFiles() map[string][]*url.URL {
	return pb.store.GetAllFiles()
}

// PutPackage method inserts a package into the appropriate buckets
func (pb *PocketBase) PutPackage(tx transactionable, id string, rev string, mark bool, overwrite bool) error {
	defer pb.delCache("count:Packages")
	defer pb.delCache("count:Marks")
	defer pb.delCache("mark:0")
	defer pb.delCache("mark:1")

	return pb.store.PutPackage(tx, id, rev, mark, overwrite)
}

// PutPackages method is a bulk method of PutPackage
func (pb *PocketBase) PutPackages(allDocs []*BarePackage) {
	tx := pb.store.AcquireTx()
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
	defer pb.delCache("count:Packages")
	defer pb.delCache("count:Documents")
	defer pb.delCache("count:Files")
	defer pb.delCache("count:Marks")
	defer pb.delCache("mark:0")
	defer pb.delCache("mark:1")

	pb.store.DeletePackage(name)
}

// PutCompleted method inserts a completed package into the appropriate buckets
func (pb *PocketBase) PutCompleted(pack *BarePackage, document string, rev string, downloads []*url.URL) (succeed bool) {
	defer pb.delCache(document)
	defer pb.delCache(document + ":rev")
	defer pb.delCache("count:Packages")
	defer pb.delCache("count:Documents")
	defer pb.delCache("count:Files")
	defer pb.delCache("count:Marks")

	tx := pb.store.AcquireTx()
	defer tx.Rollback()

	succeed = pb.store.PutCompleted(tx, pack, document, rev, downloads)

	if err := tx.Commit(); err != nil {
		succeed = false
		return
	}

	return
}
