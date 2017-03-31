package npm

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/julienschmidt/httprouter"
	"github.com/ssut/pocketnpm/db"
	"github.com/ssut/pocketnpm/log"
)

type PocketServer struct {
	db           *db.PocketBase
	serverConfig *ServerConfig
	mirrorConfig *MirrorConfig
	router       *httprouter.Router
}

func NewPocketServer(db *db.PocketBase, serverConfig *ServerConfig, mirrorConfig *MirrorConfig) *PocketServer {
	mirrorConfig.Path, _ = filepath.Abs(mirrorConfig.Path)
	if _, err := os.Stat(mirrorConfig.Path); os.IsNotExist(err) {
		log.Fatalf("Directory does not exist: %s", mirrorConfig.Path)
	}

	server := &PocketServer{
		db:           db,
		serverConfig: serverConfig,
		mirrorConfig: mirrorConfig,
		router:       httprouter.New(),
	}
	server.addRoutes()

	return server
}

// Run runs server
func (server *PocketServer) Run() {
	addr := fmt.Sprintf("%s:%d", server.serverConfig.Bind, server.serverConfig.Port)
	log.Infof("Listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, server.router))
}

func (server *PocketServer) addRoutes() {
	server.router.GET("/", server.getIndex)
	server.router.GET("/:name", server.getDocument)
	server.router.GET("/:name/:version", server.getDocumentByVersion)
	server.router.GET("/:name/:version/:tarball", server.downloadPackage)
	server.router.NotFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "{}")
	})
}

func (server *PocketServer) getIndex(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {

}

func (server *PocketServer) getDocument(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {

}

func (server *PocketServer) getDocumentByVersion(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {

}

func (server *PocketServer) downloadPackage(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {

}
