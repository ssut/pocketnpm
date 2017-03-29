package npm

import (
	"path/filepath"
	"strings"
)

func getLocalPath(base string, path string) string {
	chunks := strings.SplitAfterN(path, "/", 3)
	name := string(string([]rune(chunks[0]))[0])
	local := filepath.Join(base, name, path)

	return local
}
