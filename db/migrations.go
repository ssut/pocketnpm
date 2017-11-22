package db

import (
	"fmt"
	"strconv"

	"github.com/jinzhu/gorm"
	"github.com/ssut/pocketnpm/log"
)

var (
	DBVERSION = 1
)

type Migrator struct {
	db *gorm.DB
}

type MigrationFunc func(*gorm.DB) error

type Migration struct {
	ID          int
	Description string
	Up          MigrationFunc
	Down        MigrationFunc
}

func (m *Migrator) GetTableName(table interface{}) string {
	return m.db.NewScope(table).GetModelStruct().TableName(m.db)
}

func (m *Migrator) Version() (version int) {
	if !m.db.HasTable(&GlobalStore{}) {
		return 0
	}

	var item GlobalStore
	res := m.db.Model(&GlobalStore{Key: "dbversion"}).First(&item)
	if res.Error != nil {
		return 0
	}

	version, _ = strconv.Atoi(item.Value)
	return
}

func (m *Migrator) SetVersion(tx *gorm.DB, version int) error {
	value := strconv.Itoa(version)
	var item GlobalStore
	res := tx.Where(GlobalStore{Key: "dbversion"}).Assign(GlobalStore{Value: value}).FirstOrCreate(&item)
	return res.Error
}

func (m *Migrator) Plan() []*Migration {
	tables := []interface{}{
		&GlobalStore{},
		&PackageStore{},
		&PackageDistStore{},
	}

	p := []*Migration{
		{
			ID:          1,
			Description: "Initialize base table schema",
			Up: func(tx *gorm.DB) error {
				tx.CreateTable(tables...)
				packageTableName := m.GetTableName(&PackageStore{})
				tx.Model(&PackageDistStore{}).AddForeignKey("package_id", packageTableName+"(id)", "CASCADE", "CASCADE")

				return nil
			},
			Down: func(tx *gorm.DB) error {
				tx.DropTable(tables...)

				return nil
			},
		},
	}

	return p
}

func (m *Migrator) NeedsUpgrade(version int) bool {
	return version < DBVERSION
}

func (m *Migrator) Run() error {
	migrations := []*Migration{}
	version := m.Version()
	if m.NeedsUpgrade(version) {
		plans := m.Plan()
		if plans[len(plans)-1].ID > DBVERSION {
			return fmt.Errorf("Migration plan version mismatch")
		}

		for _, migration := range plans {
			if migration.ID > version && migration.ID <= DBVERSION {
				migrations = append(migrations, migration)
			}
		}
	}

	count := len(migrations)
	log.Printf("%d migrations will upgrade", count)
	for i, migration := range migrations {
		tx := m.db.Begin()
		migration.Up(tx)
		m.SetVersion(tx, migration.ID)
		err := tx.Commit().Error
		if err != nil {
			log.Errorf("%d of %d: %s ... %v", i+1, count, migration.Description, err)
			return err
		}
		log.Printf("%d of %d: %s ... OK", i+1, count, migration.Description)
	}

	return nil
}
