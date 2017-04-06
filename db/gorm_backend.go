package db

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/jinzhu/gorm"
	"github.com/ssut/pocketnpm/log"

	_ "github.com/jinzhu/gorm/dialects/mysql"
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

type gormStore struct {
	db     *gorm.DB
	config *DatabaseConfig
}

type gormGlobal struct {
	Key   string `gorm:"primary_key"`
	Value string
}

type gormPackage struct {
	ID       string `gorm:"primary_key"`
	Revision string
	Document string `sql:"type:mediumtext"`
	Marked   bool   `gorm:"index"`
	Files    string `sql:"type:mediumtext"`
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

func newGormStore(config *DatabaseConfig) PocketStore {
	store := &gormStore{
		config: config,
	}

	return store
}

func (store *gormStore) Connect() error {
	var err error
	attributes := store.config.Path.([]interface{})
	store.db, err = gorm.Open(attributes[0].(string), attributes[1].(string))
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
		return err
	}

	return nil
}

func (store *gormStore) Close() {
	store.db.Close()
}

func (store *gormStore) Init() {
	hasGlobals := store.db.HasTable(&gormGlobal{})
	err := store.db.AutoMigrate(&gormGlobal{}, &gormPackage{}).Error
	if err != nil {
		log.Fatalf("Failed to execute auto migration: %v", err)
	}

	if !hasGlobals {
		sequence := gormGlobal{Key: "sequence", Value: "0"}
		err = store.db.Create(&sequence).Error
		if err != nil {
			log.Fatalf("Failed to initialize: %v", err)
		}
	}
}

func (store *gormStore) IsInitialized() bool {
	hasTables := store.db.HasTable(&gormGlobal{}) && store.db.HasTable(&gormPackage{})
	if !hasTables {
		return false
	}

	notFound := store.db.Where(&gormGlobal{Key: "sequence"}).RecordNotFound()
	if notFound {
		return false
	}

	return true
}

func (store *gormStore) GetItemCount(name string) (count int) {
	store.db.Model(&gormPackage{}).Count(&count)
	return
}

func (store *gormStore) GetSequence() (sequence int) {
	var item gormGlobal
	store.db.Where(&gormGlobal{Key: "sequence"}).First(&item)

	sequence, _ = strconv.Atoi(item.Value)
	return
}

func (store *gormStore) SetSequence(seq int) {
	var item gormGlobal
	store.db.Where(&gormGlobal{Key: "sequence"}).First(&item)
	item.Value = strconv.Itoa(seq)
	store.db.Save(&item)
}

func (store *gormStore) GetCountOfMarks(cond string) (count int) {
	flag := false
	if cond == MarkComplete {
		flag = true
	}

	store.db.Model(&gormPackage{}).Where("marked = ?", flag).Count(&count)
	return
}

func (store *gormStore) GetIncompletePackages() (packages []*BarePackage) {
	rows, err := store.db.Model(&gormPackage{}).Select("id, revision, marked").Where("marked = ?", false).Rows()
	if err != nil {
		log.Fatalf("Failed to get all incomplete packages: %v", err)
		return
	}

	for rows.Next() {
		var item gormPackage
		store.db.ScanRows(rows, &item)
		pack := &BarePackage{
			ID:       item.ID,
			Revision: item.Revision,
		}

		packages = append(packages, pack)
	}

	return
}

func (store *gormStore) GetRevision(id string) string {
	var item gormPackage
	notFound := store.db.Where(&gormPackage{ID: id}).First(&item).RecordNotFound()

	if !notFound {
		return item.Revision
	}

	return ""
}

func (store *gormStore) GetDocument(id string, withfiles bool) (document string, rawfiles []byte, err error) {
	var item gormPackage
	notFound := store.db.Where(&gormPackage{ID: id}).First(&item).RecordNotFound()

	if notFound {
		err = errors.New("Package does not exist")
		return
	}

	document = item.Document
	if withfiles {
		rawfiles, _ = base64.StdEncoding.DecodeString(item.Files)
	}

	if document == "" && !item.Marked {
		err = errors.New("Package has not been downloaded yet")
		return
	}

	return
}

func (store *gormStore) GetAllFiles() (all map[string][]*url.URL) {
	rows, _ := store.db.Where(&gormPackage{}).Rows()

	for rows.Next() {
		var item gormPackage
		store.db.ScanRows(rows, &item)

		files, _ := base64.StdEncoding.DecodeString(item.Files)
		var filelist []*url.URL
		var buf bytes.Buffer
		buf.Write(files)
		dec := gob.NewDecoder(&buf)
		dec.Decode(&filelist)

		all[item.ID] = filelist
	}

	return
}

func (store *gormStore) AcquireTx() transactionable {
	tx := &gormTx{Tx: store.db.Begin()}
	return tx
}

func (store *gormStore) PutPackage(tr transactionable, id string, rev string, mark bool, overwrite bool) error {
	tx := tr.(*gormTx).Tx

	var existingPack gormPackage
	if tx.Where("id = ?", id).First(&existingPack).RecordNotFound() {
		pack := gormPackage{
			ID:       id,
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

func (store *gormStore) DeletePackage(id string) {
	store.db.Delete(&gormPackage{ID: id})
}

func (store *gormStore) PutCompleted(tr transactionable, pack *BarePackage, document string, rev string, downloads []*url.URL) bool {
	tx := tr.(*gormTx).Tx

	// get existing package
	var item gormPackage
	err := tx.Where(gormPackage{ID: pack.ID}).First(&item).Error
	if err != nil {
		return false
	}

	// if revision does not match
	if pack.Revision != rev {
		item.Revision = rev
	}
	item.Document = document
	item.Marked = true

	// encode files
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err = enc.Encode(downloads)
	if err != nil {
		return false
	}
	files := base64.StdEncoding.EncodeToString(buf.Bytes())

	item.Files = files

	err = tx.Save(&item).Error
	if err != nil {
		return false
	}

	return true
}
