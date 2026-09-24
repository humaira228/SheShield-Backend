package duress

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

func seedAlert(t *testing.T, conn *sql.DB, id string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := conn.Exec(`
		INSERT INTO users (uid, name, email, password_hash, phone, country_code, gender, user_type, is_helper_verified, created_at)
		VALUES ('victim1', 'Nadia', 'nadia@example.com', 'x', '1712345678', '+880', 'female', 'user', 0, ?)
		ON CONFLICT(uid) DO NOTHING`, now); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := conn.Exec(`
		INSERT INTO alerts (id, user_uid, latitude, longitude, accuracy_m, created_at, status)
		VALUES (?, 'victim1', 23.8, 90.4, 10, ?, 'active')`, id, now,
	); err != nil {
		t.Fatalf("seed alert: %v", err)
	}
}

func TestType_Valid(t *testing.T) {
	valid := []Type{TypeHardwarePattern, TypeSafewordVoice, TypeMissedCheckin, TypeManualPanic}
	for _, ty := range valid {
		if !ty.Valid() {
			t.Errorf("%q should be valid", ty)
		}
	}
	if Type("not_a_real_type").Valid() {
		t.Error("unknown type should not be valid")
	}
}

func TestActive_FalseBeforeAnySignalTrueAfter(t *testing.T) {
	conn := openTestDB(t)
	seedAlert(t, conn, "sos1")
	repo := NewRepository(conn)

	active, err := repo.Active("sos1")
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if active {
		t.Fatal("want false before any signal is inserted")
	}

	if _, err := repo.Insert("sos1", TypeManualPanic, time.Now()); err != nil {
		t.Fatalf("insert: %v", err)
	}

	active, err = repo.Active("sos1")
	if err != nil {
		t.Fatalf("active after insert: %v", err)
	}
	if !active {
		t.Fatal("want true after a signal is inserted")
	}
}

func TestListBySOS_OldestFirstAndScopedToOneSOS(t *testing.T) {
	conn := openTestDB(t)
	seedAlert(t, conn, "sos1")
	seedAlert(t, conn, "sos2")
	repo := NewRepository(conn)

	t0 := time.Now().UTC()
	if _, err := repo.Insert("sos1", TypeManualPanic, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Insert("sos1", TypeMissedCheckin, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Insert("sos2", TypeHardwarePattern, t0); err != nil {
		t.Fatal(err)
	}

	signals, err := repo.ListBySOS("sos1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(signals) != 2 {
		t.Fatalf("want 2 signals for sos1, got %d", len(signals))
	}
	if signals[0].Type != TypeManualPanic || signals[1].Type != TypeMissedCheckin {
		t.Errorf("want oldest-first order, got %+v", signals)
	}
}
