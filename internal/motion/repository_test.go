package motion

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/zannatulmaliha/sheshield-backend/internal/db"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	now := time.Now().UTC().Format(time.RFC3339)
	for _, u := range []string{"u1", "u2"} {
		if _, err := conn.Exec(`
			INSERT INTO users (uid, name, email, password_hash, phone, country_code, gender, user_type, is_helper_verified, created_at)
			VALUES (?, ?, ?, 'x', '1712345678', '+880', 'female', 'user', 0, ?)`, u, u, u+"@e.com", now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := conn.Exec(`INSERT INTO alerts (id, user_uid, created_at, status) VALUES ('a-u2', 'u2', ?, 'active')`, now); err != nil {
		t.Fatal(err)
	}
	return conn
}

func TestInsertListDeletePurge(t *testing.T) {
	repo := NewRepository(openTestDB(t))
	now := time.Now().UTC()

	// SOS ids that belong to someone else are dropped, not attached.
	e, err := repo.Insert("u1", ReportRequest{Type: TypeFall, Confidence: .9, UserResponse: ResponseTimeout, SOSID: "a-u2"}, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if e.SOSID != "" {
		t.Errorf("foreign sos id must be dropped, got %q", e.SOSID)
	}
	if _, err := repo.Insert("u1", ReportRequest{Type: TypeSprint, Confidence: .6, UserResponse: ResponseOK}, now.Add(time.Second), now); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Insert("u2", ReportRequest{Type: TypeStruggle, Confidence: .7, UserResponse: ResponseNone}, now, now); err != nil {
		t.Fatal(err)
	}

	mine, err := repo.ListMine("u1", 10)
	if err != nil || len(mine) != 2 || mine[0].Type != TypeSprint {
		t.Fatalf("ListMine newest-first, own only: %+v %v", mine, err)
	}

	// Retention purge only removes old rows.
	if n, err := repo.PurgeOlderThan(now.Add(time.Hour)); err != nil || n != 3 {
		t.Fatalf("purge n=%d err=%v", n, err)
	}
	_, _ = repo.Insert("u1", ReportRequest{Type: TypeFall, Confidence: .9, UserResponse: ResponseNone}, now, now)
	if n, err := repo.DeleteMine("u1"); err != nil || n != 1 {
		t.Fatalf("delete n=%d err=%v", n, err)
	}
}

func TestInsert_HourlyCapProtectsDatabase(t *testing.T) {
	repo := NewRepository(openTestDB(t))
	now := time.Now().UTC()
	for i := 0; i < maxEventsPerHour; i++ {
		if _, err := repo.Insert("u1", ReportRequest{Type: TypeSprint, Confidence: .5, UserResponse: ResponseNone}, now, now); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	if _, err := repo.Insert("u1", ReportRequest{Type: TypeSprint, Confidence: .5, UserResponse: ResponseNone}, now, now); !errors.Is(err, ErrTooMany) {
		t.Fatalf("want ErrTooMany, got %v", err)
	}
	// Another user is unaffected.
	if _, err := repo.Insert("u2", ReportRequest{Type: TypeFall, Confidence: .5, UserResponse: ResponseNone}, now, now); err != nil {
		t.Fatal(err)
	}
}
