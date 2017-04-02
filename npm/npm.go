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

	"github.com/Sirupsen/logrus"
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

func (c *NPMClient) attemptGet(url string, maxAttempts int, returnStream bool) (resp *http.Response, body interface{}, err error) {
	attempts := 0

	for {
		attempts++

		if returnStream || strings.Contains(url, "%") {
			resp, err = http.Get(url)
			if err != nil {
				log.Error(err)
				return resp, nil, err
			}

			body = resp.Body
		} else {
			var statusCode int
			statusCode, body, err = c.httpClient.Get(nil, url)
			resp = &http.Response{
				StatusCode:    statusCode,
				ContentLength: int64(len(body.([]byte))),
			}
		}

		if err != nil && attempts < maxAttempts {
			log.WithFields(logrus.Fields{
				"attempts": attempts,
			}).Warnf("http error: %s", err)
			continue
		}

		if err == nil || attempts >= maxAttempts {
			break
		}
	}

	if !returnStream {
		if _, ok := body.(io.ReadCloser); ok {
			defer resp.Body.Close()
			body, _ = ioutil.ReadAll(resp.Body)
		}
	}

	return
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
	var statusCode int
	var err error

	// if id starts with "@" then we have to use the default http client
	// of Go because fasthttp breaks it up
	if strings.HasPrefix(id, "@") {
		u.Path = fmt.Sprintf("%s/%s", u.Path, strings.Replace(id, "/", "%2F", 1))
		docURL = strings.Replace(u.String(), "%25", "%", -1)
	} else {
		u.Path = path.Join(u.Path, id)
		docURL = u.String()
	}

	resp, body, err := c.attemptGet(docURL, 3, false)

	log.Debugf("Get: %s", docURL)

	if err != nil {
		log.Error(err)
		return ""
	}
	if resp.StatusCode != fasthttp.StatusOK {
		log.Errorf("Unexpected status code: %d (%s)", statusCode, docURL)
		return ""
	}

	return string(body.([]byte))
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

	var out *os.File
	_, err = os.Stat(path)

	if err != nil {
		out, err = os.Create(path)
		if err != nil {
			log.Fatalf("Failed to create a file: %s (%q)", path, err)
		}
	} else {
		out, err = os.OpenFile(path, os.O_RDWR, 0666)
	}
	defer out.Close()

	resp, body, err := c.attemptGet(url.String(), 3, true)
	defer body.(io.ReadCloser).Close()

	if resp.StatusCode == fasthttp.StatusOK {
		size := resp.ContentLength
		stat, _ := out.Stat()
		if stat.Size() == size {
			return true
		}

		n, _ := io.Copy(out, body.(io.ReadCloser))
		return size == n
	}

	return false
}
