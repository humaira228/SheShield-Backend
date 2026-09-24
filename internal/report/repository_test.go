package report

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

func TestCreate_UserAndSystemReportsShareTheSameQueue(t *testing.T) {
	conn := openTestDB(t)
	repo := NewRepository(conn)
	now := time.Now().UTC()

	if _, err := repo.Create("u1", "u2", "user", "harassment", "", now); err != nil {
		t.Fatalf("create user report: %v", err)
	}
	if _, err := repo.Create("h1", "u2", "helper", "unsafe_behavior", "sos1", now); err != nil {
		t.Fatalf("create helper report: %v", err)
	}
	if _, err := repo.Create("system", "u2", ReporterRoleSystem, "sos_cancel_pattern", "sos2", now); err != nil {
		t.Fatalf("create system flag: %v", err)
	}

	queue, err := repo.Queue(StatusPending)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	if len(queue) != 3 {
		t.Fatalf("want all 3 reports in the one shared pending queue, got %d", len(queue))
	}
}

func TestReview_OnlyMovesOutOfPendingWhenCalled(t *testing.T) {
	conn := openTestDB(t)
	repo := NewRepository(conn)
	now := time.Now().UTC()

	rep, err := repo.Create("u1", "u2", "user", "harassment", "", now)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Still pending until a reviewer explicitly reviews it -- nothing in
	// this package moves a report on its own.
	got, err := repo.Get(rep.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ReviewStatus != StatusPending {
		t.Fatalf("want still pending, got %q", got.ReviewStatus)
	}

	if err := repo.Review(rep.ID, "moderator1", StatusActioned, "confirmed harassment, warned", now); err != nil {
		t.Fatalf("review: %v", err)
	}

	got, err = repo.Get(rep.ID)
	if err != nil {
		t.Fatalf("get after review: %v", err)
	}
	if got.ReviewStatus != StatusActioned || got.ReviewerID != "moderator1" || got.ReviewedAt == nil {
		t.Fatalf("want actioned by moderator1 with reviewed_at set, got %+v", got)
	}

	queue, err := repo.Queue(StatusPending)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	if len(queue) != 0 {
		t.Fatalf("reviewed report should have left the pending queue, still has %d", len(queue))
	}
}

func TestBlock_IdempotentAndReversible(t *testing.T) {
	conn := openTestDB(t)
	repo := NewRepository(conn)
	now := time.Now().UTC()

	if err := repo.Block("a", "b", now); err != nil {
		t.Fatalf("block: %v", err)
	}
	// Blocking twice is a no-op, not an error -- one-tap, no explanation.
	if err := repo.Block("a", "b", now); err != nil {
		t.Fatalf("block again: %v", err)
	}

	blocked, err := repo.IsBlocked("a", "b")
	if err != nil {
		t.Fatalf("is blocked: %v", err)
	}
	if !blocked {
		t.Fatal("want a and b blocked either direction")
	}
	blockedReverse, err := repo.IsBlocked("b", "a")
	if err != nil {
		t.Fatalf("is blocked reverse: %v", err)
	}
	if !blockedReverse {
		t.Fatal("IsBlocked should be symmetric regardless of argument order")
	}

	if err := repo.Unblock("a", "b"); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	blocked, err = repo.IsBlocked("a", "b")
	if err != nil {
		t.Fatalf("is blocked after unblock: %v", err)
	}
	if blocked {
		t.Fatal("want unblocked to be reversible")
	}
}
