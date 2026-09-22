package helper

import (
	"database/sql"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zannatulmaliha/sheshield-backend/internal/db"
)

// This repo's existing tests all use fakes (see service_test.go and every
// other package's *_test.go). The accept-race property can only be proven
// against the real database engine -- a fake can't exercise SQLite's actual
// locking -- so this test deliberately opens a real, temporary SQLite file
// via internal/db.Open, the same way cmd/api/main.go does.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	conn, err := db.Open(path)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestAccept_ExactlyOneWinnerUnderConcurrency(t *testing.T) {
	conn := openTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)

	// One requester, whose SOS is up for grabs.
	if _, err := conn.Exec(`
		INSERT INTO users (uid, name, email, password_hash, phone, country_code, gender, user_type, is_helper_verified, created_at)
		VALUES ('victim1', 'Nadia', 'nadia@example.com', 'x', '1712345678', '+880', 'female', 'user', 0, ?)`, now); err != nil {
		t.Fatalf("seed victim: %v", err)
	}
	if _, err := conn.Exec(`
		INSERT INTO alerts (id, user_uid, latitude, longitude, accuracy_m, created_at, status)
		VALUES ('alert1', 'victim1', 23.8, 90.4, 10, ?, 'active')`, now); err != nil {
		t.Fatalf("seed alert: %v", err)
	}

	// 25 helpers racing to accept the same alert.
	const helpers = 25
	for i := 0; i < helpers; i++ {
		if _, err := conn.Exec(`
			INSERT INTO users (uid, name, email, password_hash, phone, country_code, gender, user_type, is_helper_verified, created_at)
			VALUES (?, 'Helper', ?, 'x', '1000000000', '+880', 'helper', 'helper', 1, ?)`,
			helperUID(i), helperUID(i)+"@example.com", now); err != nil {
			t.Fatalf("seed helper %d: %v", i, err)
		}
	}

	repo := NewRepository(conn)

	var wins int64
	var wg sync.WaitGroup
	for i := 0; i < helpers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, won, err := repo.Accept("alert1", helperUID(i), time.Now())
			if err != nil {
				t.Errorf("helper %d: unexpected error: %v", i, err)
				return
			}
			if won {
				atomic.AddInt64(&wins, 1)
			}
		}(i)
	}
	wg.Wait()

	if wins != 1 {
		t.Fatalf("want exactly 1 winner out of %d concurrent accepts, got %d", helpers, wins)
	}

	var status string
	if err := conn.QueryRow(`SELECT status FROM alerts WHERE id = 'alert1'`).Scan(&status); err != nil {
		t.Fatalf("read back status: %v", err)
	}
	if status != "accepted" {
		t.Fatalf("want alert status 'accepted', got %q", status)
	}
}

func helperUID(i int) string {
	return "helper" + string(rune('a'+i))
}
