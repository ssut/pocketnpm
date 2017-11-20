package npm

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pquerna/ffjson/ffjson"
	"github.com/sirupsen/logrus"
	"github.com/valyala/fasthttp"

	"path"

	"github.com/ssut/pocketnpm/log"
)

type NPMClient struct {
	legacyClient *http.Client
	httpClient   *fasthttp.Client
	registry     string
	path         string
}

func NewNPMClient(registry string, path string) *NPMClient {
	legacyClient := &http.Client{}
	httpClient := &fasthttp.Client{
		Name: "PocketNPM Client",
	}

	client := &NPMClient{
		legacyClient: legacyClient,
		httpClient:   httpClient,
		registry:     registry,
		path:         path,
	}

	return client
}

func (c *NPMClient) attemptGet(url string, maxAttempts int) (resp *http.Response, err error) {
	attempts := 0

	for {
		attempts++

		resp, err = c.legacyClient.Get(url)
		if err != nil && attempts < maxAttempts {
			log.WithFields(logrus.Fields{
				"attempts": attempts,
			}).Warnf("http error: %v", err)
			continue
		}

		if err == nil || attempts >= maxAttempts {
			break
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

	log.Debugf("GET: %s", u.String())
	res, err := c.legacyClient.Get(u.String())
	if err != nil {
		log.Fatal(err)
		return nil
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		log.Fatalf("Unexpected status code: %d", res.StatusCode)
		return nil
	}

	var resp AllDocsResponse
	if err := ffjson.NewDecoder().DecodeReader(res.Body, &resp); err != nil {
		log.Print(err)
		log.Fatalf("Could not decode JSON data: %s", err)
		return nil
	}

	return &resp
}

func (c *NPMClient) GetDocument(id string) string {
	u, _ := url.Parse(c.registry)

	var docURL string
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

	resp, err := c.attemptGet(docURL, 3)
	log.Debugf("Get: %s", docURL)

	if err != nil {
		log.Error(err)
		return ""
	}

	if resp.StatusCode != http.StatusOK {
		log.Errorf("Unexpected status code: %d (%s)", resp.StatusCode, docURL)
		return strconv.FormatInt(int64(resp.StatusCode), 10)
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	return string(body)
}

func (c *NPMClient) GetChangesSince(seq int) *ChangesResponse {
	u, _ := url.Parse(c.registry)
	u.Path = path.Join(u.Path, "_changes")
	q := make(url.Values)
	q.Add("since", strconv.FormatInt(int64(seq), 10))
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
	if err := ffjson.Unmarshal(body, &resp); err != nil {
		log.Errorf("Could not decode JSON data: %s", err)
		return nil
	}

	return &resp
}

func (c *NPMClient) Download(url *url.URL, shasum string) bool {
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

	if _, err := out.Stat(); err == nil {
		fileHash, err := hashSHA1(out)
		if err == nil && fileHash == shasum {
			return true
		}
	}

	resp, err := c.attemptGet(url.String(), 3)
	if err != nil {
		return false
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		n, err := io.Copy(out, resp.Body)
		if err != nil {
			return false
		}
		return resp.ContentLength == n
	}

	return false
}
