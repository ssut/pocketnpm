package db

import (
	"errors"
	"fmt"
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
	db       *gorm.DB
	migrator *Migrator
	config   *DatabaseConfig
}

// GlobalStore contains important things
type GlobalStore struct {
	Key   string `gorm:"primary_key"`
	Value string
}

// PackageStore contains packages
type PackageStore struct {
	ID        []byte `gorm:"primary_key"`
	Revision  string
	Document  string              `gorm:"size:-1"`
	Completed bool                `gorm:"index"`
	Dists     []*PackageDistStore `gorm:"ForeignKey:PackageID"`
}

// IDString returns a string value of ID
func (m *PackageStore) IDString() string {
	return string(m.ID)
}

// PackageDistStore contains package dists
type PackageDistStore struct {
	Package    *PackageStore
	PackageID  []byte `gorm:"index"`
	Hash       string
	Path       string
	Downloaded bool `gorm:"index"`
}

// IDString returns a string value of ID
func (m *PackageDistStore) IDString() string {
	return string(m.PackageID)
}

type StoreTx struct {
	Tx        *gorm.DB
	committed bool
}

func (base *StoreTx) Commit() error {
	err := base.Tx.Commit().Error
	if err == nil {
		base.committed = true
	}
	return err
}

func (base *StoreTx) Rollback() error {
	if base.committed {
		return nil
	}
	err := base.Tx.Rollback().Error
	return err
}

func NewStore(config *DatabaseConfig) *Store {
	store := &Store{
		config:   config,
		migrator: &Migrator{},
	}

	return store
}

func (store *Store) Connect() error {
	var err error
	store.db, err = gorm.Open(store.config.Driver, store.config.Source)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %v", err)
	}
	if store.migrator != nil {
		store.migrator.db = store.db
	}

	return nil
}

func (store *Store) Close() error {
	return store.db.Close()
}

func (store *Store) Init() error {
	err := store.migrator.Run()
	if err != nil {
		return err
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
	if store.migrator.Version() == 0 {
		return false
	}

	if _, err := store.GetSequence(); err != nil {
		return false
	}

	version := store.migrator.Version()
	if store.migrator.NeedsUpgrade(version) {
		return false
	}

	return true
}

func (store *Store) GetStats() (stats *DatabaseStats) {
	completed, _ := store.CountPackages(true)
	packages, _ := store.GetItemCount(&PackageStore{})

	stats = &DatabaseStats{
		Packages:  packages,
		Completed: completed,
	}

	return
}

func (store *Store) GetItemCount(model interface{}) (count int64, err error) {
	res := store.db.Model(model).Count(&count)
	err = res.Error
	return
}

func (store *Store) Get(key string) (string, error) {
	var item GlobalStore
	res := store.db.Where(&GlobalStore{Key: key}).First(&item)
	if res.Error != nil {
		return "", res.Error
	}

	return item.Value, nil
}

func (store *Store) Set(key, value string) (err error) {
	var item GlobalStore
	res := store.db.Where(GlobalStore{Key: "sequence"}).Assign(GlobalStore{Value: value}).FirstOrCreate(&item)
	return res.Error
}

func (store *Store) GetSequence() (sequence int, err error) {
	seqStr, err := store.Get("sequence")
	if err != nil {
		return 0, err
	}

	sequence, err = strconv.Atoi(seqStr)
	return
}

func (store *Store) SetSequence(seq int) (err error) {
	return store.Set("sequence", strconv.Itoa(seq))
}

func (store *Store) CountPackages(cond bool) (count int64, err error) {
	flag := false
	if cond == true {
		flag = true
	}

	res := store.db.Model(&PackageStore{}).Where("completed = ?", flag).Count(&count)
	err = res.Error
	return
}

func (store *Store) GetIncompletePackages() (packages []*Package) {
	rows, err := store.db.Model(&PackageStore{}).Select("id, revision, completed").Where("completed = ?", false).Order("id").Rows()
	if err != nil {
		log.Fatalf("Failed to get all incomplete packages: %v", err)
		return
	}

	var item PackageStore
	for rows.Next() {
		store.db.ScanRows(rows, &item)
		store.db.Model(&PackageDistStore{}).Where("package_id = ? AND downloaded = ?", item.ID, true).Find(&item.Dists)
		pkg := NewPackage(item.IDString(), item.Revision)
		pkg.Dists = make([]*Dist, len(item.Dists))
		for i, dist := range item.Dists {
			pkg.Dists[i] = &Dist{
				URL:        dist.Path,
				Hash:       dist.Hash,
				Downloaded: dist.Downloaded,
			}
		}
		packages = append(packages, pkg)
	}

	return
}

func (store *Store) GetRevision(id string) string {
	var item PackageStore
	notFound := store.db.Select("revision").Where(&PackageStore{ID: []byte(id)}).First(&item).RecordNotFound()

	if !notFound {
		return item.Revision
	}

	return ""
}

func (store *Store) GetDocument(id string, withfiles bool) (document string, dists []*PackageDistStore, err error) {
	var item PackageStore
	query := store.db.Where(&PackageStore{ID: []byte(id)})
	if withfiles {
		query = query.Preload("Dists")
	}
	query = query.First(&item)

	if ok := !query.RecordNotFound(); !ok {
		err = errors.New("Package does not exist")
		return
	}

	document = item.Document
	if withfiles {
		dists = item.Dists
	}

	if document == "" && !item.Completed {
		err = errors.New("Package has not been downloaded yet")
		return
	}

	return
}

func (store *Store) GetAllFiles() (all []*PackageDistStore) {
	return
}

func (store *Store) AcquireTx() *StoreTx {
	tx := &StoreTx{Tx: store.db.Begin()}
	return tx
}

func (store *Store) AddPackage(tr *StoreTx, pkg *Package, completed bool) error {
	var selfAcquired bool
	if tr == nil {
		tr = store.AcquireTx()
		selfAcquired = true
	}
	tx := tr.Tx

	var existingPack PackageStore
	if tx.Where("id = ?", pkg.ID).First(&existingPack).RecordNotFound() {
		pack := PackageStore{
			ID:        pkg.ID,
			Revision:  pkg.Revision,
			Completed: completed,
		}
		err := tx.Create(&pack).Error
		if err != nil {
			return fmt.Errorf("Failed to create: %s %v", pkg.IDString(), err)
		}
	} else {
		existingPack.Revision = pkg.Revision
		existingPack.Completed = completed
		tx.Save(&existingPack)
	}

	if selfAcquired {
		return tr.Commit()
	}

	return nil
}

func (store *Store) DeletePackage(id string) error {
	return store.db.Delete(&PackageStore{ID: []byte(id)}).Error
}

func (store *Store) AddCompletedPackage(tr *StoreTx, pack *Package, document string, rev string, dists []*Dist) bool {
	var selfAcquired bool
	if tr == nil {
		tr = store.AcquireTx()
		selfAcquired = true
	}
	tx := tr.Tx

	// get existing package
	var item PackageStore
	err := tx.Where(PackageStore{ID: []byte(pack.ID)}).First(&item).Error
	if err != nil {
		return false
	}

	item.Revision = rev
	item.Document = document
	item.Completed = true

	var distItem PackageDistStore
	cond := PackageDistStore{
		PackageID: pack.ID,
	}
	for _, dist := range dists {
		cond.Hash = dist.Hash
		cond.Path = dist.URL
		err := tx.Where(cond).Assign(PackageDistStore{
			PackageID:  pack.ID,
			Hash:       dist.Hash,
			Path:       dist.URL,
			Downloaded: dist.Downloaded,
		}).FirstOrCreate(&distItem).Error
		if err != nil {
			return false
		}
	}

	err = tx.Save(&item).Error
	if err != nil {
		return false
	}

	if selfAcquired {
		return tr.Commit() == nil
	}

	return true
}
