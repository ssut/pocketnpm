package db

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/jinzhu/gorm"
	"github.com/ssut/pocketnpm/log"

	// GORM MySQL driver
	_ "github.com/jinzhu/gorm/dialects/mysql"
	// GORM Postgres driver
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

// Store represents a database
type Store struct {
	db     *gorm.DB
	config *DatabaseConfig
}

// GlobalStore contains important things
type GlobalStore struct {
	Key   string `gorm:"primary_key"`
	Value string
}

// PackageStore contains packages
type PackageStore struct {
	ID       []byte `gorm:"primary_key"`
	Revision string
	Document string             `gorm:"size:-1"`
	Marked   bool               `gorm:"index"`
	Dists    []PackageDistStore `gorm:"ForeignKey:PackageID"`
}

// IDString returns a string value of ID
func (m *PackageStore) IDString() string {
	return string(m.ID)
}

// PackageDistStore contains package dists
type PackageDistStore struct {
	PackageID  []byte
	Hash       string
	Path       string
	Downloaded bool `gorm:"index"`
}

// IDString returns a string value of ID
func (m *PackageDistStore) IDString() string {
	return string(m.PackageID)
}

type gormTx struct {
	Tx        *gorm.DB
	committed bool
}

func (base *gormTx) Commit() error {
	err := base.Tx.Commit().Error
	if err == nil {
		base.committed = true
	}
	return err
}

func (base *gormTx) Rollback() error {
	if base.committed {
		return nil
	}
	err := base.Tx.Rollback().Error
	return err
}

func NewStore(config *DatabaseConfig) *Store {
	store := &Store{
		config: config,
	}

	return store
}

func (store *Store) Connect() error {
	var err error
	store.db, err = gorm.Open(store.config.Driver, store.config.Source)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %v", err)
	}

	return nil
}

func (store *Store) Close() error {
	return store.db.Close()
}

func (store *Store) Init() error {
	err := store.db.AutoMigrate(&GlobalStore{}, &PackageStore{}, &PackageDistStore{}).Error
	if err != nil {
		return fmt.Errorf("Failed to execute auto migration: %v", err)
	}

	if _, err := store.GetSequence(); err != nil {
		sequence := GlobalStore{Key: "sequence", Value: "0"}
		err = store.db.Create(&sequence).Error
		if err != nil {
			return fmt.Errorf("Failed to initialize: %v", err)
		}
	}

	return nil
}

func (store *Store) IsInitialized() bool {
	tables := []interface{}{&GlobalStore{}, &PackageStore{}, &PackageDistStore{}}
	for _, table := range tables {
		if !store.db.HasTable(table) {
			return false
		}
	}

	if _, err := store.GetSequence(); err != nil {
		return false
	}

	return true
}

func (store *Store) GetStats() (stats *DatabaseStats) {
	stats = &DatabaseStats{}

	return
}

func (store *Store) GetItemCount(name string) (count int, err error) {
	res := store.db.Model(&PackageStore{}).Count(&count)
	err = res.Error
	return
}

func (store *Store) GetSequence() (sequence int, err error) {
	var item GlobalStore
	res := store.db.Where(&GlobalStore{Key: "sequence"}).First(&item)
	if res.Error != nil {
		return 0, res.Error
	}

	sequence, err = strconv.Atoi(item.Value)
	return
}

func (store *Store) SetSequence(seq int) (err error) {
	var item GlobalStore
	store.db.Where(&GlobalStore{Key: "sequence"}).First(&item)
	item.Value = strconv.Itoa(seq)
	store.db.Save(&item)
	return
}

func (store *Store) GetCountOfMarks(cond bool) (count int) {
	flag := false
	if cond == true {
		flag = true
	}

	store.db.Model(&PackageStore{}).Where("marked = ?", flag).Count(&count)
	return
}

func (store *Store) GetIncompletePackages() (packages []*Package) {
	rows, err := store.db.Model(&PackageStore{}).Select("id, revision, marked").Where("marked = ?", false).Rows()
	if err != nil {
		log.Fatalf("Failed to get all incomplete packages: %v", err)
		return
	}

	for rows.Next() {
		var item PackageStore
		store.db.ScanRows(rows, &item)
		pack := &Package{
			ID:       item.IDString(),
			Revision: item.Revision,
		}

		packages = append(packages, pack)
	}

	return
}

func (store *Store) GetRevision(id string) string {
	var item PackageStore
	notFound := store.db.Where(&PackageStore{ID: []byte(id)}).First(&item).RecordNotFound()

	if !notFound {
		return item.Revision
	}

	return ""
}

func (store *Store) GetDocument(id string, withfiles bool) (document string, files []*PackageDistStore, err error) {
	var item PackageStore
	query := store.db.Where(&PackageStore{ID: []byte(id)}).First(&item)
	if withfiles {
		query = query.Preload("PackageDistStore")
	}

	if ok := !query.RecordNotFound(); !ok {
		err = errors.New("Package does not exist")
		return
	}

	document = item.Document
	if withfiles {
	}

	if document == "" && !item.Marked {
		err = errors.New("Package has not been downloaded yet")
		return
	}

	return
}

func (store *Store) GetAllFiles() (all []*PackageDistStore) {
	return
}

func (store *Store) AcquireTx() transactionable {
	tx := &gormTx{Tx: store.db.Begin()}
	return tx
}

func (store *Store) PutPackage(tr transactionable, id string, rev string, mark bool, overwrite bool) error {
	tx := tr.(*gormTx).Tx

	var existingPack PackageStore
	if tx.Where("id = ?", id).First(&existingPack).RecordNotFound() {
		pack := PackageStore{
			ID:       []byte(id),
			Revision: rev,
			Marked:   mark,
		}
		err := tx.Create(&pack).Error
		if err != nil {
			return fmt.Errorf("Failed to create: %s %v", id, err)
		}
	} else {
		existingPack.Revision = rev
		existingPack.Marked = mark
		tx.Save(&existingPack)
	}

	return nil
}

func (store *Store) DeletePackage(id string) {
	store.db.Delete(&PackageStore{ID: []byte(id)})
}

func (store *Store) PutCompleted(tr transactionable, pack *Package, document string, rev string, downloads []*url.URL) bool {
	tx := tr.(*gormTx).Tx

	// get existing package
	var item PackageStore
	err := tx.Where(PackageStore{ID: []byte(pack.ID)}).First(&item).Error
	if err != nil {
		return false
	}

	item.Revision = rev
	item.Document = document
	item.Marked = true

	// encode files
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err = enc.Encode(downloads)
	if err != nil {
		return false
	}
	// files := base64.StdEncoding.EncodeToString(buf.Bytes())

	err = tx.Save(&item).Error
	if err != nil {
		return false
	}

	return true
}
