package npm

import (
	"os"
	"path/filepath"

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

func (c *MirrorClient) Run() {
	if !c.db.IsInitialized() {
		c.db.Init()
	}
}
