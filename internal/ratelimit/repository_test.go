package ratelimit

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
		VALUES (?, ?, ?, 'x', '1700000000', '+880', 'female', 'user', 0, ?)`,
		uid, uid, uid+"@example.com", now,
	); err != nil {
		t.Fatalf("seed user %s: %v", uid, err)
	}
}

func TestNeedsReview_BelowThresholdIsFalse(t *testing.T) {
	conn := openTestDB(t)
	seedUser(t, conn, "u1")
	repo := NewRepository(conn)
	now := time.Now()

	for i := 0; i < Threshold-1; i++ {
		if err := repo.MarkCancelled("u1", now); err != nil {
			t.Fatalf("mark cancelled: %v", err)
		}
	}

	needs, err := repo.NeedsReview("u1", now)
	if err != nil {
		t.Fatalf("needs review: %v", err)
	}
	if needs {
		t.Errorf("want false with %d cancellations (threshold is %d), got true", Threshold-1, Threshold)
	}
}

func TestNeedsReview_AtThresholdIsTrue(t *testing.T) {
	conn := openTestDB(t)
	seedUser(t, conn, "u1")
	repo := NewRepository(conn)
	now := time.Now()

	for i := 0; i < Threshold; i++ {
		if err := repo.MarkCancelled("u1", now); err != nil {
			t.Fatalf("mark cancelled: %v", err)
		}
	}

	needs, err := repo.NeedsReview("u1", now)
	if err != nil {
		t.Fatalf("needs review: %v", err)
	}
	if !needs {
		t.Errorf("want true at exactly the threshold (%d), got false", Threshold)
	}
}

// TestNeedsReview_CancelledAndFalseCountsAreAdditive matters specifically
// because the spec requires the *combined* pattern to route to review, not
// just repeated cancels in isolation -- a person who cancelled twice and was
// separately flagged false three times is exactly as review-worthy as five
// straight cancels.
func TestNeedsReview_CancelledAndFalseCountsAreAdditive(t *testing.T) {
	conn := openTestDB(t)
	seedUser(t, conn, "u1")
	repo := NewRepository(conn)
	now := time.Now()

	if err := repo.MarkCancelled("u1", now); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkCancelled("u1", now); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < Threshold-2; i++ {
		if err := repo.MarkFalse("u1", now); err != nil {
			t.Fatal(err)
		}
	}

	needs, err := repo.NeedsReview("u1", now)
	if err != nil {
		t.Fatalf("needs review: %v", err)
	}
	if !needs {
		t.Errorf("want true once cancelled+false counts sum to the threshold, got false")
	}
}

func TestNeedsReview_OutsideWindowDoesNotCount(t *testing.T) {
	conn := openTestDB(t)
	seedUser(t, conn, "u1")
	repo := NewRepository(conn)

	old := time.Now().AddDate(0, 0, -WindowDays-5)
	for i := 0; i < Threshold; i++ {
		if err := repo.MarkCancelled("u1", old); err != nil {
			t.Fatal(err)
		}
	}

	needs, err := repo.NeedsReview("u1", time.Now())
	if err != nil {
		t.Fatalf("needs review: %v", err)
	}
	if needs {
		t.Errorf("want cancellations outside the rolling window to not count, got needs review = true")
	}
}

// TestNeedsReview_DifferentUsersDoNotShareCounters guards against a query
// that accidentally sums across every user instead of filtering by user_id
// -- silent cross-user leakage would be exactly the kind of bug that turns
// this into an unfair signal.
func TestNeedsReview_DifferentUsersDoNotShareCounters(t *testing.T) {
	conn := openTestDB(t)
	seedUser(t, conn, "u1")
	seedUser(t, conn, "u2")
	repo := NewRepository(conn)
	now := time.Now()

	for i := 0; i < Threshold; i++ {
		if err := repo.MarkCancelled("u1", now); err != nil {
			t.Fatal(err)
		}
	}

	needs, err := repo.NeedsReview("u2", now)
	if err != nil {
		t.Fatalf("needs review: %v", err)
	}
	if needs {
		t.Errorf("u2 has no cancellations of their own -- want false, got true")
	}
}
