package npm

import (
	"os"
	"path/filepath"
	"sync"

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

	npmClient := NewNPMClient(config.Registry, config.Path)
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
	c.db.SetSequence(allDocs.Sequence)
	log.Debug("Succeed")
}

func (c *MirrorClient) FirstRun() {
	allDocs := c.npmClient.GetAllDocs()
	log.Infof("Total documents found: %d", allDocs.TotalRows)

	log.Debug("Store all documents by given properties")
	c.initDocument(allDocs)
}

func (c *MirrorClient) Start() {
	// Load all packages with its revision
	packages := c.db.GetImcompletePackages()

	// Array of workers
	var workers = make([]*MirrorWorker, 10)
	// Initialize channels
	var workQueue = make(chan *db.BarePackage)
	var workerQueue = make(chan chan *db.BarePackage, c.config.MaxConnections)
	var resultQueue = make(chan *MirrorWorkResult)
	// WaitGroup
	var wg sync.WaitGroup

	// Create mirror workers
	for i := 0; i < c.config.MaxConnections; i++ {
		log.Debugf("Starting worker: %d", i)
		workers[i] = NewMirrorWorker(i, c.npmClient, workerQueue, resultQueue, &wg)
		workers[i].Start()
	}

	// Result handler
	go func() {
		for {
			_, ok := <-resultQueue
			if !ok {
				return
			}

		}
	}()

	// Start dispatcher
	go func() {
		for {
			select {
			case work := <-workQueue:
				// here we received a work request
				go func(work *db.BarePackage) {
					worker := <-workerQueue

					// dispatch work request
					worker <- work
				}(work)
			}
		}
	}()

	// Dispatch all packages
	for _, pkg := range packages {
		wg.Add(1)
		workQueue <- pkg
	}

	// Wait for jobs to be finished
	wg.Wait()

	// Wait for all workers complete
	for _, worker := range workers {
		wg.Add(1)
		worker.Stop()
	}
	wg.Wait()
	log.Info("Done")
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
	markedCount := c.db.GetCountOfMarks(true)

	if seq == 0 {
		log.WithFields(logrus.Fields{
			"sequence": seq,
			"marked":   markedCount,
		}).Info("State marked as first run")
		c.FirstRun()
	}

	if seq > 0 && markedCount < stats.Packages {
		log.WithFields(logrus.Fields{
			"sequence": seq,
			"marked":   markedCount,
		}).Info("Continue")
		c.Start()
	}

	if seq > 0 && markedCount == stats.Packages {
		log.WithFields(logrus.Fields{
			"sequence": seq,
			"marked":   markedCount,
		}).Info("State marked as run for updates")
	}

}
