package npm

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/valyala/fasthttp"

	"encoding/json"

	"path"

	"github.com/ssut/pocketnpm/log"
)

type NPMClient struct {
	httpClient *fasthttp.Client
	registry   string
	path       string
}

func NewNPMClient(registry string, path string) *NPMClient {
	httpClient := &fasthttp.Client{
		Name: "PocketNPM Client",
	}

	client := &NPMClient{
		httpClient: httpClient,
		registry:   registry,
		path:       path,
	}

	return client
}

// GetAllDocs returns a list of npm packages
func (c *NPMClient) GetAllDocs() *AllDocsResponse {
	u, _ := url.Parse(c.registry)
	u.Path = path.Join(u.Path, "_all_docs")
	q := make(url.Values)
	q.Add("update_seq", "true")
	u.RawQuery = q.Encode()

	log.Debugf("Get: %s", u.String())
	statusCode, body, err := c.httpClient.Get(nil, u.String())
	if err != nil {
		log.Fatal(err)
		return nil
	}
	if statusCode != fasthttp.StatusOK {
		log.Fatalf("Unexpected status code: %d", statusCode)
		return nil
	}

	log.Debugf("Unmarshaling the entire document (%d B)", len(body))
	var resp AllDocsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		log.Print(err)
		log.Fatalf("Could not decode JSON data: %s", err)
		return nil
	}

	return &resp
}

func (c *NPMClient) GetDocument(id string) string {
	u, _ := url.Parse(c.registry)

	var docURL string
	var body []byte
	var statusCode int
	var err error

	// if id starts with "@" then we have to use the default http client
	// of Go because fasthttp breaks it up
	if strings.HasPrefix(id, "@") {
		u.Path = fmt.Sprintf("%s/%s", u.Path, strings.Replace(id, "/", "%2F", 1))
		docURL = strings.Replace(u.String(), "%25", "%", -1)

		resp, err := http.Get(docURL)
		if err != nil {
			log.Error(err)
			return ""
		}

		statusCode = resp.StatusCode
		defer resp.Body.Close()
		body, err = ioutil.ReadAll(resp.Body)
	} else {
		u.Path = path.Join(u.Path, id)
		docURL = u.String()
		statusCode, body, err = c.httpClient.Get(nil, docURL)
	}
	log.Debugf("Get: %s", docURL)

	if err != nil {
		log.Error(err)
		return ""
	}
	if statusCode != fasthttp.StatusOK {
		log.Errorf("Unexpected status code: %d (%s)", statusCode, docURL)
		return ""
	}

	return string(body)
}

func (c *NPMClient) GetChangesSince(seq int) *ChangesResponse {
	u, _ := url.Parse(c.registry)
	u.Path = path.Join(u.Path, "_changes")
	q := make(url.Values)
	q.Add("since", string(seq))
	u.RawQuery = q.Encode()

	log.Debugf("Get: %s", u.String())
	statusCode, body, err := c.httpClient.Get(nil, u.String())
	if err != nil {
		log.Error(err)
		return nil
	}
	if statusCode != fasthttp.StatusOK {
		log.Errorf("Unexpected status code: %d", statusCode)
		return nil
	}

	var resp ChangesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		log.Errorf("Could not decode JSON data: %s", err)
		return nil
	}

	return &resp
}

func (c *NPMClient) Download(url *url.URL) bool {
	path := getLocalPath(c.path, url.Path)
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		log.Fatalf("Directory is not writable: %s (%q)", path, err)
	}

	out, err := os.Create(path)
	if err != nil {
		log.Fatalf("Failed to create a file: %s (%q)", path, err)
	}
	defer out.Close()

	var resp *http.Response
	tries := 0
	for {
		tries++
		resp, err = http.Get(url.String())
		if err != nil {
			log.Warnf("HTTP error: %q", err)
		} else {
			tries = -1
		}

		if tries == -1 {
			break
		} else if tries >= 3 {
			log.Fatalf("HTTP error")
			break
		} else {
			time.Sleep(time.Second)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		size := resp.ContentLength
		n, _ := io.Copy(out, resp.Body)

		return size == n
	}

	return false
}
