package npm

import (
	"encoding/hex"
	"io"
	"path/filepath"
	"strings"

	"crypto/sha1"

	"github.com/pquerna/ffjson/ffjson"
)

type distribution struct {
	SHA1      string
	Tarball   string
	Completed bool
}

type file interface {
	io.Seeker
	io.Reader
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
			SHA1:      dist["shasum"].(string),
			Tarball:   dist["tarball"].(string),
			Completed: false,
		})
	}

	return distributions
}

func hashSHA1(f file) (sha1str string, err error) {
	hash := sha1.New()
	if _, err = io.Copy(hash, f); err != nil {
		return
	}

	hashInBytes := hash.Sum(nil)[:20]
	sha1str = hex.EncodeToString(hashInBytes)
	f.Seek(0, 0)
	return
}
