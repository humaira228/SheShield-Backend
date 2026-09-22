package alert

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zannatulmaliha/sheshield-backend/internal/db"
)

// The rest of this package's tests all use fakes (see service_test.go). The
// ownership/status WHERE clauses in UpdateLocation, Resolve and
// GetByShareToken are exactly the part a fake can't meaningfully exercise, so
// -- the same reasoning as helper/repository_test.go -- these open a real,
// temporary SQLite file via internal/db.Open, the same way cmd/api/main.go
// does.
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

// seedUser inserts a minimal, valid user row so alerts (which FK to users)
// and the GetByShareToken join have something to point at.
func seedUser(t *testing.T, conn *sql.DB, uid, name, phone string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := conn.Exec(`
		INSERT INTO users (uid, name, email, password_hash, phone, country_code, gender, user_type, is_helper_verified, created_at)
		VALUES (?, ?, ?, 'x', ?, '+880', 'female', 'user', 0, ?)`,
		uid, name, uid+"@example.com", phone, now,
	); err != nil {
		t.Fatalf("seed user %s: %v", uid, err)
	}
}

// seedAlert inserts an alert row with the given status, owner and share
// token, bypassing the service layer entirely so each test can set up
// exactly the state it wants to probe.
func seedAlert(t *testing.T, conn *sql.DB, id, ownerUID, status, shareToken string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := conn.Exec(`
		INSERT INTO alerts (id, user_uid, latitude, longitude, accuracy_m, created_at, share_token, updated_at, status)
		VALUES (?, ?, 23.8, 90.4, 10, ?, ?, ?, ?)`,
		id, ownerUID, now, shareToken, now, status,
	); err != nil {
		t.Fatalf("seed alert %s: %v", id, err)
	}
}

func TestUpdateLocation_RejectsWrongOwner(t *testing.T) {
	conn := openTestDB(t)
	seedUser(t, conn, "victim1", "Nadia Islam", "1712345678")
	seedAlert(t, conn, "alert1", "victim1", "active", "tok-1")

	repo := NewRepository(conn)
	err := repo.UpdateLocation("alert1", "attacker", f64(23.9), f64(90.5), nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound for a caller who doesn't own the alert, got %v", err)
	}

	// Make sure the mismatched call didn't sneak the update through anyway.
	var lat float64
	if err := conn.QueryRow(`SELECT latitude FROM alerts WHERE id = 'alert1'`).Scan(&lat); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if lat != 23.8 {
		t.Errorf("location must not change when the owner doesn't match, got lat=%v", lat)
	}
}

func TestUpdateLocation_RejectsNonActiveAlert(t *testing.T) {
	conn := openTestDB(t)
	seedUser(t, conn, "victim1", "Nadia Islam", "1712345678")
	seedAlert(t, conn, "alert1", "victim1", "resolved", "tok-1")

	repo := NewRepository(conn)
	err := repo.UpdateLocation("alert1", "victim1", f64(23.9), f64(90.5), nil)
	if !errors.Is(err, ErrAlertNotActive) {
		t.Fatalf("want ErrAlertNotActive for a resolved alert, got %v", err)
	}
}

func TestUpdateLocation_UpdatesOwnActiveAlert(t *testing.T) {
	conn := openTestDB(t)
	seedUser(t, conn, "victim1", "Nadia Islam", "1712345678")
	seedAlert(t, conn, "alert1", "victim1", "active", "tok-1")

	repo := NewRepository(conn)
	acc := 12.5
	if err := repo.UpdateLocation("alert1", "victim1", f64(23.9), f64(90.5), &acc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var lat, lng, accuracy float64
	if err := conn.QueryRow(
		`SELECT latitude, longitude, accuracy_m FROM alerts WHERE id = 'alert1'`,
	).Scan(&lat, &lng, &accuracy); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if lat != 23.9 || lng != 90.5 || accuracy != 12.5 {
		t.Errorf("location did not update: lat=%v lng=%v acc=%v", lat, lng, accuracy)
	}
}

func TestResolve_IdempotentSafe(t *testing.T) {
	conn := openTestDB(t)
	seedUser(t, conn, "victim1", "Nadia Islam", "1712345678")
	seedAlert(t, conn, "alert1", "victim1", "active", "tok-1")

	repo := NewRepository(conn)

	if err := repo.Resolve("alert1", "victim1"); err != nil {
		t.Fatalf("first Resolve: unexpected error: %v", err)
	}

	var status string
	var resolvedAt sql.NullString
	if err := conn.QueryRow(
		`SELECT status, resolved_at FROM alerts WHERE id = 'alert1'`,
	).Scan(&status, &resolvedAt); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "resolved" || !resolvedAt.Valid {
		t.Fatalf("want status=resolved with resolved_at set, got status=%q resolved_at.Valid=%v", status, resolvedAt.Valid)
	}
	firstResolvedAt := resolvedAt.String

	// Calling it again must not panic or corrupt the row -- it should simply
	// report the alert is no longer active, and leave resolved_at untouched.
	err := repo.Resolve("alert1", "victim1")
	if !errors.Is(err, ErrAlertNotActive) {
		t.Fatalf("second Resolve: want ErrAlertNotActive, got %v", err)
	}
	if err := conn.QueryRow(
		`SELECT status, resolved_at FROM alerts WHERE id = 'alert1'`,
	).Scan(&status, &resolvedAt); err != nil {
		t.Fatalf("read back after second Resolve: %v", err)
	}
	if status != "resolved" || resolvedAt.String != firstResolvedAt {
		t.Errorf("second Resolve must not change the row: status=%q resolved_at=%q (was %q)", status, resolvedAt.String, firstResolvedAt)
	}
}

func TestGetByShareToken_NotFoundForGarbageToken(t *testing.T) {
	conn := openTestDB(t)
	repo := NewRepository(conn)

	_, err := repo.GetByShareToken("this-token-does-not-exist")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound for an unknown token, got %v", err)
	}
}

func TestGetByShareToken_ReturnsOnlyPublicFields(t *testing.T) {
	conn := openTestDB(t)
	const distinctivePhone = "1799999999"
	seedUser(t, conn, "victim1", "Nadia Islam", distinctivePhone)
	seedAlert(t, conn, "alert1", "victim1", "active", "tok-xyz")

	repo := NewRepository(conn)
	view, err := repo.GetByShareToken("tok-xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.FirstName != "Nadia" {
		t.Errorf("want first name only (\"Nadia\"), got %q", view.FirstName)
	}
	if view.Status != "active" {
		t.Errorf("want status active, got %q", view.Status)
	}
	if view.Latitude == nil || *view.Latitude != 23.8 || view.Longitude == nil || *view.Longitude != 90.4 {
		t.Errorf("unexpected coordinates: %+v", view)
	}
	if view.UpdatedAt.IsZero() {
		t.Errorf("want a non-zero UpdatedAt")
	}

	// Belt and suspenders: the struct has no phone/email/full-name field to
	// begin with, but also confirm nothing identifying leaks through even if
	// someone later adds one carelessly.
	data, _ := json.Marshal(view)
	if strings.Contains(string(data), distinctivePhone) || strings.Contains(string(data), "Islam") {
		t.Errorf("public view must never include phone number or last name, got: %s", data)
	}
}
