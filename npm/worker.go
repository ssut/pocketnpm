package npm

import (
	"net/url"
	"sync"

	"regexp"

	"github.com/ssut/pocketnpm/db"
	"github.com/ssut/pocketnpm/log"
)

var (
	// ExpRegistryFile defines the URL format for registry file
	ExpRegistryFile = regexp.MustCompile(`"(?P<url>http:\/\/registry.npmjs.org([a-zA-Z0-9.\-_\/]+))"`)
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
	Package  *db.BarePackage
	Document string
	Files    []*url.URL
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
				// - fetch document from the registry with given name and revision
				// - parse all urls in the document
				// - download all packages ends with `.tgz`
				// then, result handler:
				// - put document into the bucket Documents
				// - put file list into the bucket Files
				// - mark the package as completed (commit)
				document := w.npmClient.GetDocument(work.ID, work.Revision)
				downloads := []*url.URL{}

				// find possible urls
				urls := ExpRegistryFile.FindAllStringSubmatch(document, -1)
				for _, u := range urls {
					path := u[len(u)-1]
					download := &url.URL{
						Scheme: "https",
						Host:   "registry.npmjs.org",
						Path:   path,
					}
					downloads = append(downloads, download)
				}

				// download all files here

				w.ResultQueue <- &MirrorWorkResult{
					Package:  work,
					Document: document,
					Files:    downloads,
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
