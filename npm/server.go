package npm

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buaazp/fasthttprouter"
	"github.com/pquerna/ffjson/ffjson"
	"github.com/sirupsen/logrus"
	"github.com/ssut/pocketnpm/db"
	"github.com/ssut/pocketnpm/log"
	"github.com/valyala/fasthttp"
)

// PocketServer type contains essential shared items to run a npm server
type PocketServer struct {
	store        *db.Store
	serverConfig *ServerConfig
	mirrorConfig *MirrorConfig
	router       *fasthttprouter.Router
	logger       *logrus.Logger
}

// NewPocketServer initializes new instance of PocketServer
func NewPocketServer(store *db.Store, serverConfig *ServerConfig, mirrorConfig *MirrorConfig) *PocketServer {
	mirrorConfig.Path, _ = filepath.Abs(mirrorConfig.Path)
	if _, err := os.Stat(mirrorConfig.Path); os.IsNotExist(err) {
		log.Fatalf("Directory does not exist: %s", mirrorConfig.Path)
	}

	logger := logrus.New()
	if logPath := serverConfig.LogPath; logPath != "" {
		logPath, _ = filepath.Abs(logPath)
		file, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			file, err = os.Create(logPath)
		}

		if err == nil {
			logger.Out = file
			logger.Formatter = &logrus.TextFormatter{
				FullTimestamp:    true,
				DisableColors:    true,
				QuoteEmptyFields: true,
			}
		} else {
			log.Warnf("Failed to open log file: %s", logPath)
		}
	} else {
		logger = nil
	}

	server := &PocketServer{
		store:        store,
		serverConfig: serverConfig,
		mirrorConfig: mirrorConfig,
		router:       fasthttprouter.New(),
		logger:       logger,
	}
	server.addRoutes()

	return server
}

// Run runs server
func (server *PocketServer) Run() error {
	addr := fmt.Sprintf("%s:%d", server.serverConfig.Bind, server.serverConfig.Port)
	log.Infof("Listening on %s", addr)
	err := fasthttp.ListenAndServe(addr, server.router.Handler)
	return err
}

func (server *PocketServer) addRoutes() {
	server.router.GET("/", server.logging(server.getIndex))
	server.router.GET("/:name", server.logging(server.getDocument))
	server.router.GET("/:name/:version", server.logging(server.getDocumentByVersion))
	server.router.GET("/:name/:version/:tarball", server.logging(server.downloadPackage))
	server.router.GET("/:name/:version/:tarball/:extra", server.logging(server.downloadPackage))
	server.router.NotFound = server.raiseNotFound
	server.router.PanicHandler = server.handlePanic
}

func (server *PocketServer) logging(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	serverName := fmt.Sprintf("PocketNPM (%s)", runtime.Version())
	return fasthttp.RequestHandler(func(ctx *fasthttp.RequestCtx) {
		ctx.Response.Header.SetServer(serverName)

		start := time.Now()
		next(ctx)
		elapsed := time.Since(start)

		if server.logger == nil {
			return
		}

		server.logger.WithFields(logrus.Fields{
			"IP":         ctx.RemoteIP().String(),
			"StatusCode": ctx.Response.StatusCode(),
			"Elapsed":    elapsed.String(),
			"User-Agent": string(ctx.UserAgent()),
		}).Info(fmt.Sprintf(`%s %s`, ctx.Method(), ctx.Path()))

	})
}

func (server *PocketServer) raiseNotFound(ctx *fasthttp.RequestCtx) {
	ctx.SetStatusCode(404)
	ctx.Write([]byte("{}"))
}

func (server *PocketServer) handlePanic(ctx *fasthttp.RequestCtx, panic interface{}) {
	ctx.SetStatusCode(500)
	log.Debugf("%v: %s", panic, debug.Stack())
}

func (server *PocketServer) writeJSON(ctx *fasthttp.RequestCtx, content interface{}) {
	json, err := ffjson.Marshal(content)
	if err != nil {
		ctx.SetStatusCode(500)
		return
	}
	ctx.SetBody(json)
}

func (server *PocketServer) sendFile(ctx *fasthttp.RequestCtx, path string, name string) {
	stat, err := os.Stat(path)
	if err != nil {
		log.Debug(err)
		ctx.SetStatusCode(404)
		return
	}

	size := strconv.FormatInt(stat.Size(), 10)

	ctx.SetContentType("application/octet-stream")
	ctx.Response.Header.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	ctx.Response.Header.Set("Content-Length", size)

	if server.serverConfig.Redirect {
		internalPath := strings.Replace(path, server.mirrorConfig.Path, server.serverConfig.RedirectPath, 1)
		ctx.Response.Header.Set("X-Accel-Redirect", internalPath)
		return
	}

	ctx.SendFile(path)
}

func (server *PocketServer) replaceAttachments(document string) string {
	// ReplaceAllStringFunc is considered to be slow
	dists := getDistributions(document)
	replaces := make([]string, len(dists)*2)

	var i int
	for _, dist := range dists {
		u, _ := url.Parse(dist.Tarball)
		u.Scheme = server.serverConfig.Scheme
		u.Host = server.serverConfig.Host

		replaces[i] = dist.Tarball
		replaces[i+1] = u.String()
		i += 2
	}

	replacer := strings.NewReplacer(replaces...)
	document = replacer.Replace(document)

	return document
}

func (server *PocketServer) getDocumentByName(ctx *fasthttp.RequestCtx, name string) string {
	rev := server.store.GetRevision(name)
	etag := fmt.Sprintf(`"%s"`, rev)
	ctx.Response.Header.Set("ETag", etag)

	if cacheHeader := ctx.Request.Header.Peek("If-None-Match"); cacheHeader != nil {
		if string(cacheHeader) == etag {
			ctx.SetStatusCode(304)
			return ""
		}
	} else {
		ctx.Response.Header.Set("Cache-Control", "must-revalidate")
	}

	doc, _, err := server.store.GetDocument(name, false)
	if err != nil {
		ctx.SetStatusCode(404)
		server.writeJSON(ctx, map[string]string{
			"error": err.Error(),
		})
		return ""
	}
	doc = server.replaceAttachments(doc)

	return doc
}

func (server *PocketServer) getIndex(ctx *fasthttp.RequestCtx) {
	stat := server.store.GetStats()
	markedCount, _ := server.store.CountPackages(true)
	sequence, _ := server.store.GetSequence()
	output := map[string]interface{}{
		"packages":  stat.Packages,
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
	ctx.WriteString(doc)
}

func (server *PocketServer) getDocumentByVersion(ctx *fasthttp.RequestCtx) {
	name, version := ctx.UserValue("name").(string), ctx.UserValue("version").(string)
	if strings.HasPrefix(name, "@") && !strings.Contains(name, "/") {
		ctx.SetUserValue("name", fmt.Sprintf("%s/%s", name, version))
		server.getDocument(ctx)
		return
	}

	doc := server.getDocumentByName(ctx, name)

	if doc == "" {
		return
	}

	var jsonDoc interface{}
	ffjson.Unmarshal([]byte(doc), &jsonDoc)
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
	name, version := ctx.UserValue("name").(string), ctx.UserValue("version").(string)
	tarball, extra := ctx.UserValue("tarball").(string), ctx.UserValue("extra")

	if strings.HasPrefix(name, "@") && extra == nil {
		ctx.SetUserValue("name", fmt.Sprintf("%s/%s", name, version))
		ctx.SetUserValue("version", tarball)
		server.getDocumentByVersion(ctx)
		return
	}

	if extra != nil {
		name = fmt.Sprintf("%s/%s", name, version)
		version = tarball
		tarball = extra.(string)
	}

	if version != "-" {
		server.raiseNotFound(ctx)
		return
	}
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
