package config

import (
	"bufio"
	"os"
	"strings"
)

// loadDotEnv reads KEY=VALUE lines from path into the process environment.
// A variable that is already set in the real environment wins over the file,
// so production can still inject secrets the normal way. A missing file is
// not an error -- it just means "use the real environment".
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if len(val) >= 2 {
			first, last := val[0], val[len(val)-1]
			if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if _, alreadySet := os.LookupEnv(key); !alreadySet {
			_ = os.Setenv(key, val)
		}
	}
}
