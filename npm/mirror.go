package npm

import (
	"os"
	"path/filepath"

	"github.com/Sirupsen/logrus"
	"github.com/ssut/pocketnpm/db"
	"github.com/ssut/pocketnpm/log"
)

type MirrorClient struct {
	db        *db.PocketBase
	config    *MirrorConfig
	npmClient *NPMClient
}

func NewMirrorClient(db *db.PocketBase, config *MirrorConfig) *MirrorClient {
	// Fix relative path
	config.Path, _ = filepath.Abs(config.Path)
	// Check for directory exists or not
	// If not, try to create an empty directory for it
	if _, err := os.Stat(config.Path); os.IsNotExist(err) {
		log.Debugf("Directory does not exist: %s", config.Path)
		err = os.MkdirAll(config.Path, 0755)
		if err != nil {
			log.Fatalf("Failed to create directory: %s", config.Path)
		} else {
			log.Debugf("Directory has been created: %s", config.Path)
		}
	}

	npmClient := NewNPMClient(config.Registry)
	client := &MirrorClient{
		config:    config,
		db:        db,
		npmClient: npmClient,
	}

	return client
}

func (c *MirrorClient) initDocument(allDocs *AllDocsResponse) {
	packages := make([]*db.BarePackage, allDocs.TotalRows)

	for i, doc := range allDocs.Rows {
		packages[i] = &db.BarePackage{
			ID:       doc.ID,
			Revision: doc.Value.Revision,
		}
	}

	log.Debug("Putting packages..")
	c.db.PutPackages(packages)
	log.Debug("Succeed")
}

func (c *MirrorClient) FirstRun() {
	allDocs := c.npmClient.GetAllDocs()
	log.Infof("Total documents found: %d", allDocs.TotalRows)

	log.Debug("Store all documents by given properties")
	c.initDocument(allDocs)

	log.Debug("")
}

func (c *MirrorClient) Run() {
	if !c.db.IsInitialized() {
		log.Debug("Database has not been initialized. Init..")
		c.db.Init()
	}

	if !c.db.IsInitialized() {
		log.Fatal("Failed to initialize database")
	}

	stats := c.db.GetStats()
	log.WithFields(logrus.Fields{
		"Packages":  stats.Packages,
		"Marks":     stats.Marks,
		"Documents": stats.Documents,
		"Files":     stats.Files,
	}).Debug("Status for database")

	seq := c.db.GetSequence()
	if seq == 0 {
		log.Debugf("State marked as first run because sequence is zero")
		c.FirstRun()
		return
	}

	markedCount := c.db.GetCountOfMarks(true)
	if seq > 0 && markedCount == stats.Packages {
		log.WithFields(logrus.Fields{
			"sequence": string(seq),
			"marked":   string(markedCount),
		}).Debugf("State marked as run for updates")
	}

}
