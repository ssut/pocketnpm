package npm

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/ssut/pocketnpm/db"
	"github.com/ssut/pocketnpm/log"
	pbar "gopkg.in/cheggaaa/pb.v1"
)

type MirrorClient struct {
	db        *db.PocketBase
	config    *MirrorConfig
	npmClient *NPMClient
}

// NewMirrorClient creates an instance of MirrorClient
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
	packages := c.db.GetIncompletePackages()

	log.Debugf("Packages to queue: %d", len(packages))

	// Array of workers
	var workers = make([]*MirrorWorker, c.config.MaxConnections)
	// Initialize channels
	// workQueue has a buffer size of 100
	var workQueue = make(chan *db.BarePackage, 100)
	var workerQueue = make(chan chan *db.BarePackage, c.config.MaxConnections)
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
	go func(db *db.PocketBase, wg *sync.WaitGroup) {
		for {
			result, done := <-resultQueue
			if !done {
				wg.Done()
				return
			}

			if result.Deleted {
				path := getLocalPath(c.config.Path, result.Package.ID)
				os.RemoveAll(path)
				db.DeletePackage(result.Package.ID)
				log.WithFields(logrus.Fields{
					"worker": result.WorkerID,
				}).Infof("Deleted: %s", result.Package.ID)
				wg.Done()
				continue
			}

			var files []*url.URL
			for _, dist := range result.Distributions {
				if !dist.Completed {
					continue
				}
				file, _ := url.Parse(dist.Tarball)
				files = append(files, file)
			}
			succeed := db.PutCompleted(result.Package, result.Document, result.DocumentRevision, files)
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
	}(c.db, &wg)

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
		since := c.db.GetSequence()
		changes := c.npmClient.GetChangesSince(since)
		if since == changes.LastSequence {
			log.Info("Update: currently up to date. no packages will be updated")
			time.Sleep(interval)
			continue
		}

		updates := map[string]*db.BarePackage{}
		for _, pkg := range changes.Results {
			name := pkg.ID
			rev := pkg.Changes[0].Revision
			currentRev := c.db.GetRevision(name)

			if currentRev == "" || currentRev != rev {
				updates[name] = &db.BarePackage{
					ID:       name,
					Revision: rev,
				}
			}
		}

		i := 0
		packages := make([]*db.BarePackage, len(updates))
		for _, pkg := range updates {
			packages[i] = pkg
			i++
		}

		log.Infof("Update: %d packages will be updated", len(packages))

		// Put all packages
		c.db.PutPackages(packages)
		// Update sequence
		c.db.SetSequence(changes.LastSequence)
		log.Debugf("Update: Sequence has been set to %d (was %d)", changes.LastSequence, since)

		// Start worker
		c.Start()
		log.Info("Update: finish")

		time.Sleep(interval)
	}
}

func (c *MirrorClient) initialize() {
	if !c.db.IsInitialized() {
		log.Debug("Database has not been initialized. Init..")
		c.db.Init()
	} else {
		log.Debug("Database has already been initialized.")
	}

	if !c.db.IsInitialized() {
		log.Fatal("Failed to initialize database")
	}
}

func (c *MirrorClient) Run(onetime bool) {
	c.initialize()

	log.Debug("Loading stats..")
	stats := c.db.GetStats()
	log.WithFields(logrus.Fields{
		"Packages":  stats.Packages,
		"Marks":     stats.Marks,
		"Documents": stats.Documents,
		"Files":     stats.Files,
	}).Debug("Status for database")

	seq := c.db.GetSequence()

	if seq == 0 {
		log.WithFields(logrus.Fields{
			"sequence": seq,
			"marked":   0,
		}).Info("State marked as first run")
		c.FirstRun()
		c.Start()
	}

	markedCount := c.db.GetCountOfMarks(true)

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

	seq = c.db.GetSequence()

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

func (c *MirrorClient) Check() {
	c.initialize()

	// Load all files
	log.Infof("Loading all files")
	files := c.db.GetAllFiles()

	var errs []string

	count := len(files)
	log.Infof("Checking files for %d packages", count)
	bar := pbar.StartNew(count)

	// Check file exists
	for name, items := range files {
		bar.Total += int64(len(items))
		for _, item := range items {
			path := getLocalPath(c.config.Path, item.Path)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				errstr := fmt.Sprintf("%s: %s", name, path)
				errs = append(errs, errstr)
			}

			bar.Increment()
		}
		bar.Increment()
	}
	bar.Finish()

	// Write errors
	log.Debugf("Creating missing report of file checks: report.txt")
	out, _ := os.Create("report.log")
	defer out.Close()
	out.WriteString(strings.Join(errs[:], "\n"))
}
