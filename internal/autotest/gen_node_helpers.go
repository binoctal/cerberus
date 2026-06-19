package autotest

import (
	"path/filepath"
	"strconv"
	"strings"
)

func nodeTestFilePath(file string) string {
	dir := filepath.Dir(file)
	base := filepath.Base(file)

	for _, ext := range []string{".jsx", ".tsx", ".ts", ".js"} {
		if strings.HasSuffix(base, ext) {
			name := strings.TrimSuffix(base, ext)
			return filepath.Join(dir, name+".test"+ext)
		}
	}

	return filepath.Join(dir, base+".test.js")
}

func parseIntOrZero(s string) int {
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return int(i)
}
