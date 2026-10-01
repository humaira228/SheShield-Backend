package sosmsg

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/zannatulmaliha/sheshield-backend/internal/db"
)

func seed(t *testing.T, status string) (*Repository, *sql.DB) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	now := time.Now().UTC().Format(time.RFC3339)
	for _, u := range []string{"req", "help", "other"} {
		if _, err := conn.Exec(`
			INSERT INTO users (uid, name, email, password_hash, phone, country_code, gender, user_type, is_helper_verified, created_at)
			VALUES (?, ?, ?, 'x', '1712345678', '+880', 'female', 'user', 0, ?)`, u, u, u+"@e.com", now); err != nil {
			t.Fatal(err)
		}
	}
	holder := any(nil)
	if status == "accepted" {
		holder = "help"
	}
	if _, err := conn.Exec(`INSERT INTO alerts (id, user_uid, created_at, status, accepted_by_uid) VALUES ('s1', 'req', ?, ?, ?)`, now, status, holder); err != nil {
		t.Fatal(err)
	}
	return NewRepository(conn), conn
}

func TestChat_OnlyRequesterAndHolderHaveAccess(t *testing.T) {
	repo, _ := seed(t, "accepted")
	now := time.Now().UTC()

	if _, err := repo.Send("s1", "req", "please hurry", now); err != nil {
		t.Fatal(err)
	}
	m, err := repo.Send("s1", "help", "2 minutes away", now)
	if err != nil || m.From != "helper" {
		t.Fatalf("%+v %v", m, err)
	}
	if _, err := repo.Send("s1", "other", "hi", now); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("stranger must be denied, got %v", err)
	}
	if _, err := repo.List("s1", "other", 0, now); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("stranger must not read, got %v", err)
	}

	msgs, err := repo.List("s1", "help", 0, now)
	if err != nil || len(msgs) != 2 || msgs[0].From != "requester" || msgs[0].Mine || !msgs[1].Mine {
		t.Fatalf("%+v %v", msgs, err)
	}
	after, _ := repo.List("s1", "req", msgs[0].Seq, now)
	if len(after) != 1 || after[0].From != "helper" {
		t.Fatalf("cursor: %+v", after)
	}
}

func TestChat_AccessRevokedWhenResolved(t *testing.T) {
	repo, conn := seed(t, "accepted")
	now := time.Now().UTC()
	if _, err := conn.Exec(`UPDATE alerts SET status = 'resolved', resolved_at = ? WHERE id = 's1'`, now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Send("s1", "help", "still there?", now); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("helper must lose access on resolve, got %v", err)
	}
	if _, err := repo.Send("s1", "req", "thanks", now); !errors.Is(err, ErrClosed) {
		t.Fatalf("requester can read but not write after resolve, got %v", err)
	}
	if _, err := repo.List("s1", "req", 0, now); err != nil {
		t.Fatalf("requester may still read shortly after: %v", err)
	}
	if _, err := repo.List("s1", "req", 0, now.Add(readAfterResolve+time.Minute)); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("read window must expire, got %v", err)
	}
}

func TestChat_FlagsContactExtractionButStillDelivers(t *testing.T) {
	repo, conn := seed(t, "accepted")
	now := time.Now().UTC()
	if _, err := repo.Send("s1", "help", "call me 01712345678", now); err != nil {
		t.Fatal(err)
	}
	var flagged int
	if err := conn.QueryRow(`SELECT flagged FROM sos_messages LIMIT 1`).Scan(&flagged); err != nil || flagged != 1 {
		t.Fatalf("flagged=%d err=%v", flagged, err)
	}
}

func TestChat_RateLimit(t *testing.T) {
	repo, _ := seed(t, "accepted")
	now := time.Now().UTC()
	for i := 0; i < maxPerMinute; i++ {
		if _, err := repo.Send("s1", "req", "x", now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.Send("s1", "req", "x", now); !errors.Is(err, ErrRateLimit) {
		t.Fatalf("want ErrRateLimit, got %v", err)
	}
}
