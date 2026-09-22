package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

var ErrNotFound = errors.New("user not found")
var ErrDuplicateEmail = errors.New("an account with this email already exists")

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func newUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (r *Repository) Create(u User, passwordHash string) (User, error) {
	u.UID = newUID()
	u.CreatedAt = time.Now().UTC()

	_, err := r.db.Exec(`
		INSERT INTO users (uid, name, email, password_hash, phone, country_code, gender, user_type, is_helper_verified, fcm_token, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.UID, u.Name, u.Email, passwordHash, u.Phone, u.CountryCode, u.Gender, u.UserType, u.IsHelperVerified, u.FCMToken, u.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return User{}, ErrDuplicateEmail
		}
		return User{}, err
	}
	return u, nil
}

// FindByEmail also returns the stored password hash, needed only for
// sign-in's bcrypt comparison — never serialized back to the client.
func (r *Repository) FindByEmail(email string) (User, string, error) {
	row := r.db.QueryRow(`
		SELECT uid, name, email, password_hash, phone, country_code, address, gender, user_type, is_helper_verified, fcm_token, created_at
		FROM users WHERE email = ?`, email)
	return scanUserWithHash(row)
}

func (r *Repository) FindByUID(uid string) (User, error) {
	row := r.db.QueryRow(`
		SELECT uid, name, email, password_hash, phone, country_code, address, gender, user_type, is_helper_verified, fcm_token, created_at
		FROM users WHERE uid = ?`, uid)
	u, _, err := scanUserWithHash(row)
	return u, err
}

func scanUserWithHash(row *sql.Row) (User, string, error) {
	var u User
	var hash, createdAt string
	err := row.Scan(&u.UID, &u.Name, &u.Email, &hash, &u.Phone, &u.CountryCode, &u.Address, &u.Gender, &u.UserType, &u.IsHelperVerified, &u.FCMToken, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrNotFound
	}
	if err != nil {
		return User{}, "", err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return u, hash, nil
}

func isUniqueConstraintErr(err error) bool {
	// modernc.org/sqlite wraps sqlite's error text rather than exposing a
	// typed constraint error -- matching the message is the accepted approach
	// with this driver.
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// UpdateProfile saves the editable profile fields. Nothing else about the
// account (email, gender, role, verified flag) can be written from here.
func (r *Repository) UpdateProfile(u User) error {
	res, err := r.db.Exec(
		`UPDATE users SET name = ?, phone = ?, country_code = ?, address = ? WHERE uid = ?`,
		u.Name, u.Phone, u.CountryCode, u.Address, u.UID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateFCMToken saves the caller's current push registration token, so an
// SOS from someone who has linked this account as a trusted contact can
// reach this device with an alarm push. token == "" clears it (e.g. on
// sign-out), so a stale token from a previous install never gets pushed to.
func (r *Repository) UpdateFCMToken(uid, token string) error {
	res, err := r.db.Exec(`UPDATE users SET fcm_token = ? WHERE uid = ?`, nullIfEmpty(token), uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
