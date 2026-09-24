package contact

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

// TestAreConnected_SymmetricEitherDirectionLinked is the property
// internal/helper.Service's §10 mutual-connection gate depends on: A having
// B as a linked trusted contact counts as connected, and so does the
// reverse -- IsConnected(A, B) must equal IsConnected(B, A) regardless of
// which direction the link actually was.
func TestAreConnected_SymmetricEitherDirectionLinked(t *testing.T) {
	conn := openTestDB(t)
	seedUser(t, conn, "alice")
	seedUser(t, conn, "bob")
	seedUser(t, conn, "carol")
	repo := NewRepository(conn)

	// alice has bob as a linked trusted contact.
	if _, err := conn.Exec(`
		INSERT INTO trusted_contacts (id, user_uid, name, phone, country_code, created_at, linked_user_uid)
		VALUES ('tc1', 'alice', 'Bob', '1700000000', '+880', ?, 'bob')`, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("seed trusted contact: %v", err)
	}

	connected, err := repo.AreConnected("alice", "bob")
	if err != nil {
		t.Fatalf("AreConnected(alice, bob): %v", err)
	}
	if !connected {
		t.Error("want alice and bob connected (alice -> bob link)")
	}

	connectedReverse, err := repo.AreConnected("bob", "alice")
	if err != nil {
		t.Fatalf("AreConnected(bob, alice): %v", err)
	}
	if !connectedReverse {
		t.Error("want AreConnected symmetric regardless of argument order")
	}

	connectedStranger, err := repo.AreConnected("alice", "carol")
	if err != nil {
		t.Fatalf("AreConnected(alice, carol): %v", err)
	}
	if connectedStranger {
		t.Error("want alice and carol NOT connected -- no link between them")
	}
}
