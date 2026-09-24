package matching

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/zannatulmaliha/sheshield-backend/internal/db"
)

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

func seedUser(t *testing.T, conn *sql.DB, uid string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := conn.Exec(`
		INSERT INTO users (uid, name, email, password_hash, phone, country_code, gender, user_type, is_helper_verified, created_at)
		VALUES (?, ?, ?, 'x', '1700000000', '+880', 'female', 'helper', 1, ?)`,
		uid, uid+"@example.com", uid+"@example.com", now,
	); err != nil {
		t.Fatalf("seed user %s: %v", uid, err)
	}
}

func seedAlert(t *testing.T, conn *sql.DB, id, ownerUID string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := conn.Exec(`
		INSERT INTO alerts (id, user_uid, latitude, longitude, accuracy_m, created_at, status)
		VALUES (?, ?, 23.8, 90.4, 10, ?, 'active')`,
		id, ownerUID, now,
	); err != nil {
		t.Fatalf("seed alert %s: %v", id, err)
	}
}

func TestUpsertPending_IdempotentAndDoesNotOverwriteStatus(t *testing.T) {
	conn := openTestDB(t)
	seedUser(t, conn, "victim1")
	seedUser(t, conn, "helper1")
	seedAlert(t, conn, "sos1", "victim1")

	repo := NewRepository(conn)
	if err := repo.UpsertPending("sos1", "helper1"); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	// A helper who already won (or lost) shouldn't be silently reset to
	// pending just because a later poll includes the same alert again.
	if err := repo.Lock("sos1", "helper1", time.Now()); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if err := repo.UpsertPending("sos1", "helper1"); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	matches, err := repo.ListBySOS("sos1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(matches) != 1 || matches[0].Status != StatusLocked {
		t.Fatalf("want exactly one locked match, got %+v", matches)
	}
}

func TestLockAndReleaseOthers_ExactlyOneLockedRestReleased(t *testing.T) {
	conn := openTestDB(t)
	seedUser(t, conn, "victim1")
	seedAlert(t, conn, "sos1", "victim1")
	for _, h := range []string{"helper1", "helper2", "helper3"} {
		seedUser(t, conn, h)
	}

	repo := NewRepository(conn)
	for _, h := range []string{"helper1", "helper2", "helper3"} {
		if err := repo.UpsertPending("sos1", h); err != nil {
			t.Fatalf("upsert %s: %v", h, err)
		}
	}

	now := time.Now().UTC()
	if err := repo.Lock("sos1", "helper2", now); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if err := repo.ReleaseOthers("sos1", "helper2", now); err != nil {
		t.Fatalf("release others: %v", err)
	}

	matches, err := repo.ListBySOS("sos1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(matches) != 3 {
		t.Fatalf("want 3 match rows, got %d", len(matches))
	}
	locked, released := 0, 0
	for _, m := range matches {
		switch {
		case m.HelperID == "helper2" && m.Status == StatusLocked && m.AccessLevel == AccessPrecise:
			locked++
		case m.HelperID != "helper2" && m.Status == StatusReleased:
			released++
		default:
			t.Errorf("unexpected match state: %+v", m)
		}
	}
	if locked != 1 || released != 2 {
		t.Fatalf("want exactly 1 locked + 2 released, got locked=%d released=%d", locked, released)
	}
}

func TestRelease_ReopensLockedMatchAndReturnsItsHelper(t *testing.T) {
	conn := openTestDB(t)
	seedUser(t, conn, "victim1")
	seedUser(t, conn, "helper1")
	seedAlert(t, conn, "sos1", "victim1")

	repo := NewRepository(conn)
	now := time.Now().UTC()
	if err := repo.Lock("sos1", "helper1", now); err != nil {
		t.Fatalf("lock: %v", err)
	}

	released, err := repo.Release("sos1", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if released != "helper1" {
		t.Fatalf("want released helper 'helper1', got %q", released)
	}

	matches, err := repo.ListBySOS("sos1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(matches) != 1 || matches[0].Status != StatusReleased || matches[0].AccessLevel != AccessNone {
		t.Fatalf("want the one match released with no access, got %+v", matches)
	}

	// Releasing again when nothing is locked is a no-op, not an error.
	released, err = repo.Release("sos1", now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("second release: %v", err)
	}
	if released != "" {
		t.Fatalf("want no helper released the second time, got %q", released)
	}
}
