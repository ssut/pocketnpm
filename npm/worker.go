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
				document := w.npmClient.GetDocument(work.IDString())

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
							Distributions:    nil,
							WorkerID:         w.ID,
							Deleted:          true,
						}
						continue
					}
				}

				// find possible urls
				possibles := getDistributions(document)
				existsCount := 0
				// filter
				distributions := []*distribution{}
				for _, possible := range possibles {
					exists := false
					for _, dist := range work.Dists {
						if dist.Downloaded && possible.Tarball == dist.URL && possible.SHA1 == dist.Hash {
							existsCount++
							exists = true
							break
						}
					}

					if !exists {
						distributions = append(distributions, possible)
					}
				}

				// download all files here
				log.WithFields(logrus.Fields{
					"name":   work.IDString(),
					"worker": w.ID,
					"exists": existsCount,
				}).Debugf("Total files to download: %d", len(distributions))

				for _, dist := range distributions {
					file, err := url.Parse(dist.Tarball)
					if err == nil && checkValidDist(dist) {
						dist.Completed = w.npmClient.Download(file, dist.SHA1)
						if !dist.Completed {
							log.WithFields(logrus.Fields{
								"ID": work.IDString(),
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
