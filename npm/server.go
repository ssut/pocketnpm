package npm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	server.router.NotFound = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		server.raiseNotFound(w)
	})
}

func (server *PocketServer) raiseNotFound(w http.ResponseWriter) {
	w.WriteHeader(404)
	w.Write([]byte("{}"))
}

func (server *PocketServer) writeJson(w http.ResponseWriter, content interface{}) {
	json, err := json.Marshal(content)
	if err != nil {
		w.WriteHeader(500)
		return
	}
	w.Write(json)
}

func (server *PocketServer) sendFile(w http.ResponseWriter, path string, name string) {
	open, err := os.Open(path)
	defer open.Close()
	if err != nil {
		log.Debug(err)
		http.Error(w, "File not found", 404)
		return
	}

	stat, _ := open.Stat()
	size := strconv.FormatInt(stat.Size(), 10)

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", size)

	open.Seek(0, 0)
	io.Copy(w, open)
}

func (server *PocketServer) replaceAttachments(document string) string {
	// ReplaceAllStringFunc is considered to be slow
	urls := ExpRegistryFile.FindAllStringSubmatch(document, -1)
	for _, u := range urls {
		origin := u[0]
		path := u[4]
		fixed := fmt.Sprintf(`"tarball":"%s://%s/%s"`, server.serverConfig.Scheme, server.serverConfig.Host, path)

		document = strings.Replace(document, origin, fixed, 1)
	}

	return document
}

func (server *PocketServer) getIndex(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	stat := server.db.GetStats()
	markedCount := server.db.GetCountOfMarks(true)
	output := map[string]interface{}{
		"docs":      stat.Documents,
		"available": markedCount,
	}

	server.writeJson(w, &output)
}

func (server *PocketServer) getDocument(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	name := ps.ByName("name")
	doc, _, err := server.db.GetDocument(name, false)
	if err != nil {
		server.writeJson(w, map[string]string{
			"error": err.Error(),
		})
	}
	doc = server.replaceAttachments(doc)

	fmt.Fprintf(w, doc)
}

func (server *PocketServer) getDocumentByVersion(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {

}

func (server *PocketServer) downloadPackage(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	if ps.ByName("version") != "-" {
		server.raiseNotFound(w)
		return
	}
	name, tarball := ps.ByName("name"), ps.ByName("tarball")
	// Illegal access
	if strings.Contains(name, "..") || strings.Contains(tarball, "..") {
		server.raiseNotFound(w)
		return
	}

	path := fmt.Sprintf("%s/-/%s", ps.ByName("name"), ps.ByName("tarball"))
	local := getLocalPath(server.mirrorConfig.Path, path)
	// Illegal access
	if !strings.Contains(local, server.mirrorConfig.Path) {
		server.raiseNotFound(w)
		return
	}

	server.sendFile(w, local, tarball)
}
