package npm

import (
	"path/filepath"
	"strings"

	"github.com/pquerna/ffjson/ffjson"
)

type distribution struct {
	SHA1    string
	Tarball string
}

func getLocalPath(base string, path string) string {
	if strings.HasPrefix(path, "/") {
		path = path[1:len(path)]
	}
	chunks := strings.SplitAfterN(path, "/", 3)
	name := string(string([]rune(chunks[0]))[0])
	local := filepath.Join(base, name, path)

	return local
}

func getDistributions(document string) []*distribution {
	var doc interface{}
	err := ffjson.Unmarshal([]byte(document), &doc)
	if err != nil {
		return nil
	}

	var distributions []*distribution

	versions := doc.(map[string]interface{})["versions"].(map[string]interface{})
	for _, version := range versions {
		dist := version.(map[string]interface{})["dist"].(map[string]interface{})
		distributions = append(distributions, &distribution{
			SHA1:    dist["shasum"].(string),
			Tarball: dist["tarball"].(string),
		})
	}

	return distributions
}
