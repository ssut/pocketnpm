package npm

import (
	"net/url"
	"sync"

	"regexp"

	"encoding/json"

	"github.com/Sirupsen/logrus"
	"github.com/ssut/pocketnpm/db"
	"github.com/ssut/pocketnpm/log"
)

var (
	// ExpRegistryFile defines the URL format for registry file
	ExpRegistryFile = regexp.MustCompile(`"tarball":\s?"((https?):\/\/([\w0-9\.]+)\/([a-zA-Z0-9.\-_\/\@]+\.tgz))"`)
)

// MirrorWorker contains channels used to act as a worker
type MirrorWorker struct {
	ID          int
	Work        chan *db.BarePackage
	WorkerQueue chan chan *db.BarePackage
	ResultQueue chan *MirrorWorkResult
	WaitGroup   *sync.WaitGroup
	QuitChan    chan bool

	npmClient *NPMClient
}

// MirrorWorkResult contains the result of worker action
type MirrorWorkResult struct {
	Package          *db.BarePackage
	DocumentRevision string
	Document         string
	Files            []*url.URL
	WorkerID         int
}

// NewMirrorWorker creates a worker with given parameters
func NewMirrorWorker(id int, npmClient *NPMClient, workerQueue chan chan *db.BarePackage, resultQueue chan *MirrorWorkResult, wg *sync.WaitGroup) *MirrorWorker {
	worker := &MirrorWorker{
		ID:          id,
		Work:        make(chan *db.BarePackage),
		WorkerQueue: workerQueue,
		ResultQueue: resultQueue,
		WaitGroup:   wg,
		QuitChan:    make(chan bool),
		npmClient:   npmClient,
	}

	return worker
}

// Start method starts the worker by starting a goroutine
func (w *MirrorWorker) Start() {
	go func() {
		for {
			w.WorkerQueue <- w.Work

			select {
			case work := <-w.Work:
				// Workflow:
				// - fetch document from the registry with given name
				// - compare the revision with existing revision
				//   - if it doesnt match, fix it
				//   - the revision will be updated when updating database content
				// - parse all urls in the document
				// - download all packages ends with `.tgz`
				// then, result handler:
				// - put document into the bucket Documents
				// - put file list into the bucket Files
				// - mark the package as completed (commit)
				log.WithFields(logrus.Fields{
					"worker": w.ID,
				}).Infof("Mirroring: %s", work.ID)
				document := w.npmClient.GetDocument(work.ID)
				downloads := []*url.URL{}

				var doc DocumentResponse
				err := json.Unmarshal([]byte(document), &doc)
				if err != nil {
					log.Warnf("Failed to decode JSON document: %s (%v)", work.ID, err)
				}

				// find possible urls
				urls := ExpRegistryFile.FindAllStringSubmatch(document, -1)
				for _, u := range urls {
					scheme, host, path := u[2], u[3], u[4]
					download := &url.URL{
						Scheme: scheme,
						Host:   host,
						Path:   path,
					}
					downloads = append(downloads, download)
				}

				// download all files here
				log.WithFields(logrus.Fields{
					"name":   work.ID,
					"worker": w.ID,
				}).Debugf("Total files to download: %d", len(downloads))
				for _, file := range downloads {
					result := w.npmClient.Download(file)
					if !result {
						log.WithFields(logrus.Fields{
							"ID": work.ID,
						}).Warnf("Failed to download: %s", file.Path)
					}
				}

				w.ResultQueue <- &MirrorWorkResult{
					Package:          work,
					DocumentRevision: doc.Revision,
					Document:         document,
					Files:            downloads,
					WorkerID:         w.ID,
				}
				w.WaitGroup.Done()
			case <-w.QuitChan:
				log.Printf("Worker %d Stopping", w.ID)
				w.WaitGroup.Done()
				return
			}
		}
	}()
}

// Stop function tells the worker to stop listening for work requests
//
// Note that the worker will only stop after it has finished its work
func (w *MirrorWorker) Stop() {
	go func() {
		w.QuitChan <- true
	}()
}
