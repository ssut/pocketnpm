package npm

import (
	"net/url"
	"sync"

	"github.com/pquerna/ffjson/ffjson"
	"github.com/sirupsen/logrus"
	"github.com/ssut/pocketnpm/db"
	"github.com/ssut/pocketnpm/log"
)

// MirrorWorker contains channels used to act as a worker
type MirrorWorker struct {
	ID          int
	Work        chan *db.Package
	WorkerQueue chan chan *db.Package
	ResultQueue chan *MirrorWorkResult
	WaitGroup   *sync.WaitGroup
	QuitChan    chan bool

	npmClient *NPMClient
}

// MirrorWorkResult contains the result of worker action
type MirrorWorkResult struct {
	Package          *db.Package
	DocumentRevision string
	Document         string
	Distributions    []*distribution
	WorkerID         int
	Deleted          bool
}

// NewMirrorWorker creates a worker with given parameters
func NewMirrorWorker(id int, npmClient *NPMClient, workerQueue chan chan *db.Package, resultQueue chan *MirrorWorkResult, wg *sync.WaitGroup) *MirrorWorker {
	worker := &MirrorWorker{
		ID:          id,
		Work:        make(chan *db.Package),
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
				distributions := []*distribution{}

				var doc DocumentResponse
				err := ffjson.Unmarshal([]byte(document), &doc)
				if err != nil {
					log.Warnf("Failed to decode JSON document: %s (%v)", work.ID, err)

					// doc has been deleted
					if document == "404" {
						w.ResultQueue <- &MirrorWorkResult{
							Package:          work,
							DocumentRevision: "",
							Document:         document,
							Distributions:    distributions,
							WorkerID:         w.ID,
							Deleted:          true,
						}
						continue
					}
				}

				// find possible urls
				distributions = getDistributions(document)

				// download all files here
				log.WithFields(logrus.Fields{
					"name":   work.ID,
					"worker": w.ID,
				}).Debugf("Total files to download: %d", len(distributions))
				for _, dist := range distributions {
					file, _ := url.Parse(dist.Tarball)

					if checkValidDist(dist) {
						dist.Completed = w.npmClient.Download(file, dist.SHA1)
						if !dist.Completed {
							log.WithFields(logrus.Fields{
								"ID": work.ID,
							}).Warnf("Failed to download: %s", file.Path)
						}
					}
				}

				w.ResultQueue <- &MirrorWorkResult{
					Package:          work,
					DocumentRevision: doc.Revision,
					Document:         document,
					Distributions:    distributions,
					WorkerID:         w.ID,
				}
			case <-w.QuitChan:
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
