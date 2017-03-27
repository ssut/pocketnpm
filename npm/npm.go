package npm

import (
	"net/url"

	"github.com/valyala/fasthttp"

	"encoding/json"

	"path"

	"github.com/ssut/pocketnpm/log"
)

type NPMClient struct {
	httpClient *fasthttp.Client
	registry   string
}

func NewNPMClient(registry string) *NPMClient {
	httpClient := &fasthttp.Client{
		Name: "PocketNPM Client",
	}

	client := &NPMClient{
		httpClient: httpClient,
		registry:   registry,
	}

	return client
}

// GetAllDocs returns a list of npm packages
func (c *NPMClient) GetAllDocs() *AllDocsResponse {
	u, _ := url.Parse(c.registry)
	u.Path = path.Join(u.Path, "_all_docs")
	q := new(url.Values)
	q.Add("update_seq", "true")
	u.RawQuery = q.Encode()

	statusCode, body, err := c.httpClient.Get(nil, u.String())
	if err != nil {
		log.Error(err)
		return nil
	}
	if statusCode != fasthttp.StatusOK {
		log.Errorf("Unexpected status code: %d", statusCode)
		return nil
	}

	var resp *AllDocsResponse
	if err := json.Unmarshal(body, resp); err != nil {
		log.Errorf("Could not decode JSON data: %s", err)
		return nil
	}

	return resp
}

func (c *NPMClient) GetDocument(id string) string {
	u, _ := url.Parse(c.registry)
	u.Path = path.Join(u.Path, url.PathEscape(id))

	statusCode, body, err := c.httpClient.Get(nil, u.String())
	if err != nil {
		log.Error(err)
		return ""
	}
	if statusCode != fasthttp.StatusOK {
		log.Errorf("Unexpected status code: %d", statusCode)
		return ""
	}

	return string(body)
}

func (c *NPMClient) GetChangesSince(seq int) *ChangesResponse {
	u, _ := url.Parse(c.registry)
	u.Path = path.Join(u.Path, "_changes")
	q := new(url.Values)
	q.Add("since", string(seq))
	u.RawQuery = q.Encode()

	statusCode, body, err := c.httpClient.Get(nil, u.String())
	if err != nil {
		log.Error(err)
		return nil
	}
	if statusCode != fasthttp.StatusOK {
		log.Errorf("Unexpected status code: %d", statusCode)
		return nil
	}

	var resp *ChangesResponse
	if err := json.Unmarshal(body, resp); err != nil {
		log.Errorf("Could not decode JSON data: %s", err)
		return nil
	}

	return resp
}
