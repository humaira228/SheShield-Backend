package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	os.WriteFile(p, []byte("# comment\nPORT=9999\nJWT_SECRET=\"quoted secret\"\nexport DB_PATH=./x.db\n\nBROKENLINE\nPRESET=fromfile\n"), 0o600)
	os.Unsetenv("PORT"); os.Unsetenv("JWT_SECRET"); os.Unsetenv("DB_PATH")
	os.Setenv("PRESET", "fromenv")

	loadDotEnv(p)

	for k, want := range map[string]string{"PORT": "9999", "JWT_SECRET": "quoted secret", "DB_PATH": "./x.db", "PRESET": "fromenv"} {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	loadDotEnv(filepath.Join(dir, "missing.env")) // must not panic
}
