package npm

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/buaazp/fasthttprouter"
	"github.com/ssut/pocketnpm/db"
	"github.com/ssut/pocketnpm/log"
	"github.com/valyala/fasthttp"
)

// PocketServer type contains essential shared items to run a npm server
type PocketServer struct {
	db           *db.PocketBase
	serverConfig *ServerConfig
	mirrorConfig *MirrorConfig
	router       *fasthttprouter.Router
}

// NewPocketServer initializes new instance of PocketServer
func NewPocketServer(db *db.PocketBase, serverConfig *ServerConfig, mirrorConfig *MirrorConfig) *PocketServer {
	mirrorConfig.Path, _ = filepath.Abs(mirrorConfig.Path)
	if _, err := os.Stat(mirrorConfig.Path); os.IsNotExist(err) {
		log.Fatalf("Directory does not exist: %s", mirrorConfig.Path)
	}

	server := &PocketServer{
		db:           db,
		serverConfig: serverConfig,
		mirrorConfig: mirrorConfig,
		router:       fasthttprouter.New(),
	}
	server.addRoutes()

	return server
}

// Run runs server
func (server *PocketServer) Run() {
	addr := fmt.Sprintf("%s:%d", server.serverConfig.Bind, server.serverConfig.Port)
	log.Infof("Listening on %s", addr)
	log.Fatal(fasthttp.ListenAndServe(addr, server.router.Handler))
}

func (server *PocketServer) addRoutes() {
	server.router.GET("/", server.getIndex)
	server.router.GET("/:name", server.getDocument)
	server.router.GET("/:name/:version", server.getDocumentByVersion)
	server.router.GET("/:name/:version/:tarball", server.downloadPackage)
	server.router.NotFound = server.raiseNotFound
}

func (server *PocketServer) raiseNotFound(ctx *fasthttp.RequestCtx) {
	ctx.SetStatusCode(404)
	ctx.Write([]byte("{}"))
}

func (server *PocketServer) writeJSON(ctx *fasthttp.RequestCtx, content interface{}) {
	json, err := json.Marshal(content)
	if err != nil {
		ctx.SetStatusCode(500)
		return
	}
	ctx.Write(json)
}

func (server *PocketServer) sendFile(ctx *fasthttp.RequestCtx, path string, name string) {
	open, err := os.Open(path)
	defer open.Close()
	if err != nil {
		log.Debug(err)
		ctx.SetStatusCode(404)
		return
	}

	stat, _ := open.Stat()
	size := strconv.FormatInt(stat.Size(), 10)

	ctx.SetContentType("application/octet-stream")
	ctx.Response.Header.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	ctx.Response.Header.Set("Content-Length", size)

	if server.serverConfig.EnableXAccel {
		internalPath := strings.Replace(path, server.mirrorConfig.Path, "/_internal", 1)
		ctx.Response.Header.Set("X-Accel-Redirect", internalPath)
		return
	}

	open.Seek(0, 0)
	io.Copy(ctx, open)
}

func (server *PocketServer) replaceAttachments(document string) string {
	// ReplaceAllStringFunc is considered to be slow
	urls := ExpRegistryFile.FindAllStringSubmatch(document, -1)
	replaces := make([]string, len(urls)*2)

	var i int
	for _, u := range urls {
		origin := u[0]
		path := u[4]
		fixed := fmt.Sprintf(`"tarball":"%s://%s/%s"`, server.serverConfig.Scheme, server.serverConfig.Host, path)

		replaces[i] = origin
		replaces[i+1] = fixed
		i += 2
	}

	replacer := strings.NewReplacer(replaces...)
	document = replacer.Replace(document)

	return document
}

func (server *PocketServer) getDocumentByName(ctx *fasthttp.RequestCtx, name string) string {
	doc, _, err := server.db.GetDocument(name, false)
	if err != nil {
		server.writeJSON(ctx, map[string]string{
			"error": err.Error(),
		})
		return ""
	}
	doc = server.replaceAttachments(doc)

	return doc
}

func (server *PocketServer) getIndex(ctx *fasthttp.RequestCtx) {
	stat := server.db.GetStats()
	markedCount := server.db.GetCountOfMarks(true)
	sequence := server.db.GetSequence()
	output := map[string]interface{}{
		"docs":      stat.Documents,
		"available": markedCount,
		"sequence":  sequence,
	}

	server.writeJSON(ctx, &output)
}

func (server *PocketServer) getDocument(ctx *fasthttp.RequestCtx) {
	name := ctx.UserValue("name").(string)
	doc := server.getDocumentByName(ctx, name)
	size := strconv.FormatInt(int64(len(doc)), 10)

	ctx.SetContentType("application/json")
	ctx.Response.Header.Set("Content-Length", size)
	fmt.Fprint(ctx, doc)
}

func (server *PocketServer) getDocumentByVersion(ctx *fasthttp.RequestCtx) {
	name, version := ctx.UserValue("name").(string), ctx.UserValue("version").(string)
	doc := server.getDocumentByName(ctx, name)

	var jsonDoc interface{}
	json.Unmarshal([]byte(doc), &jsonDoc)
	root := jsonDoc.(map[string]interface{})
	distTags := root["dist-tags"].(map[string]interface{})
	versions := root["versions"].(map[string]interface{})
	versionKeys := make([]string, 0, len(versions))
	for k := range versions {
		versionKeys = append(versionKeys, k)
	}

	var versionDoc interface{}

	// found in dist-tags or version tree
	if val, ok := distTags[version]; ok {
		versionDoc = versions[val.(string)]
	} else if val, ok := versions[version]; ok {
		versionDoc = val
	} else {
		// parse special version name such as "^1.0.0" (above 1.0.0), "~1.0.0"("=1.0.0"), and just "2" (above 2.0.0).
		filter := string(version[0])
		versionStr := version[1:len(version)]
		if filter == "~" || filter == "=" {
			versionDoc = versions[versionStr]
		} else { // ^ (above)
			if filter != "^" {
				versionStr = version
			}
			key := strings.Split(versionStr, ".")[0]
			sort.Slice(versionKeys, func(i, j int) bool {
				return versionKeys[i] > versionKeys[j]
			})

			for _, ver := range versionKeys {
				if strings.HasPrefix(ver, key) {
					versionDoc = versions[ver]
					break
				}
			}
		}
	}

	if versionDoc == nil {
		server.raiseNotFound(ctx)
		return
	}

	server.writeJSON(ctx, versionDoc)
}

func (server *PocketServer) downloadPackage(ctx *fasthttp.RequestCtx) {
	if ctx.UserValue("version") != "-" {
		server.raiseNotFound(ctx)
		return
	}
	name, tarball := ctx.UserValue("name").(string), ctx.UserValue("tarball").(string)
	// Illegal access
	if strings.Contains(name, "..") || strings.Contains(tarball, "..") {
		server.raiseNotFound(ctx)
		return
	}

	path := fmt.Sprintf("%s/-/%s", name, tarball)
	local := getLocalPath(server.mirrorConfig.Path, path)
	// Illegal access
	if !strings.Contains(local, server.mirrorConfig.Path) {
		server.raiseNotFound(ctx)
		return
	}

	server.sendFile(ctx, local, tarball)
}
