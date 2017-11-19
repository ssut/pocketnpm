package npm

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/ssut/pocketnpm/db"
	"github.com/ssut/pocketnpm/log"
	pb "gopkg.in/cheggaaa/pb.v1"
)

type MirrorClient struct {
	store     *db.Store
	config    *MirrorConfig
	npmClient *NPMClient
}

// NewMirrorClient creates an instance of MirrorClient
func NewMirrorClient(store *db.Store, config *MirrorConfig) *MirrorClient {
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
		store:     store,
		npmClient: npmClient,
	}

	return client
}

func (c *MirrorClient) initDocument(allDocs *AllDocsResponse) {
	log.Debug("Using transaction (10000 items per one transaction)")
	log.WithFields(logrus.Fields{
		"docs": allDocs.TotalRows,
		"seq":  allDocs.Sequence,
	}).Infof("Targets")

	var trans bool
	var tx *db.StoreTx
	var checkpoint int
	bar := pb.StartNew(allDocs.TotalRows)
	for i, doc := range allDocs.Rows {
		if !trans {
			trans = true
			tx = c.store.AcquireTx()
		}

		pkg := db.NewPackage(doc.ID, doc.Value.Revision)
		c.store.AddPackage(tx, pkg, false)
		bar.Increment()

		if trans && (i+1)%10000 == 0 {
			tx.Commit()
			trans = false
			checkpoint = (i + 1) % 10000
		}
	}

	if trans {
		tx.Commit()
	}
	bar.Finish()
	c.store.SetSequence(allDocs.Sequence)
	log.Infof("Successfully initialized %d documents in %d transactions", allDocs.TotalRows, checkpoint)
}

func (c *MirrorClient) FirstRun() {
	allDocs := c.npmClient.GetAllDocs()
	log.Infof("Total documents found: %d", allDocs.TotalRows)

	log.Debug("Store all documents by given properties")
	c.initDocument(allDocs)
}

func (c *MirrorClient) Start() {
	// Load all packages with its revision
	packages := c.store.GetIncompletePackages()

	log.Debugf("Packages to queue: %d", len(packages))

	// Array of workers
	var workers = make([]*MirrorWorker, c.config.MaxConnections)
	// Initialize channels
	// workQueue has a buffer size of 100
	var workQueue = make(chan *db.Package, 100)
	var workerQueue = make(chan chan *db.Package, c.config.MaxConnections)
	var resultQueue = make(chan *MirrorWorkResult)
	// WaitGroup
	var wg sync.WaitGroup

	// Create mirror workers
	log.Debugf("Starting %d workers", c.config.MaxConnections)
	for i := 0; i < c.config.MaxConnections; i++ {
		workers[i] = NewMirrorWorker(i, c.npmClient, workerQueue, resultQueue, &wg)
		workers[i].Start()
	}

	// Result handler
	go func(wg *sync.WaitGroup) {
		for {
			result, done := <-resultQueue
			if !done {
				wg.Done()
				return
			}

			if result.Deleted {
				path := getLocalPath(c.config.Path, result.Package.IDString())
				os.RemoveAll(path)
				c.store.DeletePackage(result.Package.IDString())
				log.WithFields(logrus.Fields{
					"worker": result.WorkerID,
				}).Infof("Deleted: %s", result.Package.ID)
				wg.Done()
				continue
			}

			var dists []*db.Dist
			for _, dist := range result.Distributions {
				if !dist.Completed {
					continue
				}
				dist := &db.Dist{
					Hash:       dist.SHA1,
					URL:        dist.Tarball,
					Downloaded: dist.Completed,
				}
				dists = append(dists, dist)
			}

			var succeed bool
			if len(result.Document) > 0 {
				succeed = c.store.AddCompletedPackage(nil, result.Package, result.Document, result.DocumentRevision, dists)
			}

			wg.Done()
			if succeed {
				log.WithFields(logrus.Fields{
					"sameRev": result.Package.Revision == result.DocumentRevision,
					"files":   len(result.Distributions),
					"worker":  result.WorkerID,
				}).Infof("Mirrored: %s", result.Package.ID)
			} else {
				log.Errorf("Failed to mirror: %s", result.Package.ID)
			}
		}
	}(&wg)

	// Start dispatcher
	go func() {
		for {
			select {
			case work := <-workQueue:
				// here we received a work request
				// goroutine won't be created until the acquired worker is released
				worker := <-workerQueue
				// dispatch work request
				worker <- work
			}
		}
	}()

	// Dispatch all packages
	for _, pkg := range packages {
		wg.Add(1)
		workQueue <- pkg
	}
	log.Debug("Successfully dispatched all queues")

	// Wait for jobs to be finished
	wg.Wait()

	// Wait for all workers complete
	log.Debugf("Stopping %d workers", len(workers))
	for _, worker := range workers {
		wg.Add(1)
		worker.Stop()
	}
	wg.Wait()
	log.Info("Done")
}

func (c *MirrorClient) Update() {
	interval := time.Duration(c.config.Interval) * time.Second
	for {
		// Load changes
		since, _ := c.store.GetSequence()
		changes := c.npmClient.GetChangesSince(since)
		if since == changes.LastSequence {
			log.Info("Update: currently up to date. no packages will be updated")
			time.Sleep(interval)
			continue
		}

		updates := map[string]*db.Package{}
		for _, pkg := range changes.Results {
			name := pkg.ID
			rev := pkg.Changes[0].Revision
			currentRev := c.store.GetRevision(name)

			if currentRev == "" || currentRev != rev {
				updates[name] = db.NewPackage(name, rev)
			}
		}

		i := 0
		packages := make([]*db.Package, len(updates))
		for _, pkg := range updates {
			packages[i] = pkg
			i++
		}

		log.Infof("Update: %d packages will be updated", len(packages))

		// Put all packages
		// c.store.PutPackages(packages)
		// Update sequence
		c.store.SetSequence(changes.LastSequence)
		log.Debugf("Update: Sequence has been set to %d (was %d)", changes.LastSequence, since)

		// Start worker
		c.Start()
		log.Info("Update: finish")

		time.Sleep(interval)
	}
}

func (c *MirrorClient) initialize() {
	if !c.store.IsInitialized() {
		log.Debug("Database has not been initialized. Init..")
		err := c.store.Init()
		if err != nil {
			log.Fatal("Failed to initialize database")
		}
	} else {
		log.Debug("Database has already been initialized.")
		return
	}

	if !c.store.IsInitialized() {
		log.Fatal("Failed to initialize database")
	}
}

func (c *MirrorClient) Run(onetime bool) {
	c.initialize()

	log.Debug("Loading stats..")
	stats := c.store.GetStats()
	log.WithFields(logrus.Fields{
		"Packages":  stats.Packages,
		"Completed": stats.Completed,
		"Files":     stats.Files,
	}).Info("Status for database")

	seq, _ := c.store.GetSequence()
	if seq == 0 {
		log.WithFields(logrus.Fields{
			"sequence": seq,
			"marked":   0,
		}).Info("State marked as first run")
		c.FirstRun()
		c.Start()
	}

	markedCount, _ := c.store.CountPackages(true)

	if seq > 0 && markedCount < stats.Packages {
		log.WithFields(logrus.Fields{
			"sequence": seq,
			"marked":   markedCount,
		}).Info("Continue")
		c.Start()
	}

	if onetime {
		return
	}

	seq, _ = c.store.GetSequence()

	if seq > 0 && markedCount == stats.Packages {
		log.WithFields(logrus.Fields{
			"sequence": seq,
			"marked":   markedCount,
		}).Info("State marked as run for updates")
		go c.Update()
	}

	exit := make(chan struct{}, 1)
	<-exit
}
