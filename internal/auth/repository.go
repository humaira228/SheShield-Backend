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

// deviceFingerprint is stored but deliberately not a field on User -- see
// the spec's note that it must never be shown to any party. Keeping it out
// of the User struct entirely (rather than just an omitted JSON tag) means
// it structurally cannot leak into AuthResponse by accident.
func (r *Repository) Create(u User, passwordHash, deviceFingerprint string) (User, error) {
	u.UID = newUID()
	u.CreatedAt = time.Now().UTC()

	_, err := r.db.Exec(`
		INSERT INTO users (uid, name, email, password_hash, phone, country_code, gender, user_type, is_helper_verified, fcm_token, created_at, device_fingerprint)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.UID, u.Name, u.Email, passwordHash, u.Phone, u.CountryCode, u.Gender, u.UserType, u.IsHelperVerified, u.FCMToken, u.CreatedAt.Format(time.RFC3339), deviceFingerprint,
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return User{}, ErrDuplicateEmail
		}
		return User{}, err
	}
	return u, nil
}

// SetDiscoverable flips the requester-side half of the §10 double opt-in --
// defaults false at signup (see 014_trust_safety_fields.sql) and must be an
// explicit, later choice, never something turned on for them.
func (r *Repository) SetDiscoverable(uid string, discoverable bool) error {
	res, err := r.db.Exec(`UPDATE users SET discoverable_via_mutual_connections = ? WHERE uid = ?`, discoverable, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// IsDiscoverable reads the requester-side §10 opt-in flag -- kept as its
// own narrow read (like DeviceFingerprint) rather than added to the shared
// User struct/scan, since the only caller is internal/helper's
// mutual-connection check, not anything that serializes a full User.
func (r *Repository) IsDiscoverable(uid string) (bool, error) {
	var discoverable bool
	err := r.db.QueryRow(`SELECT discoverable_via_mutual_connections FROM users WHERE uid = ?`, uid).Scan(&discoverable)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	return discoverable, err
}

// DeviceFingerprint reads a stored device_fingerprint -- the one place it's
// ever retrieved. Callers must log this access to audit_log themselves
// (see internal/auth.Service.DeviceFingerprint), since this repository
// method has no actor identity to attribute the read to.
func (r *Repository) DeviceFingerprint(uid string) (string, error) {
	var fp string
	err := r.db.QueryRow(`SELECT device_fingerprint FROM users WHERE uid = ?`, uid).Scan(&fp)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return fp, err
}

// FindByEmail also returns the stored password hash, needed only for
// sign-in's bcrypt comparison — never serialized back to the client.
func (r *Repository) FindByEmail(email string) (User, string, error) {
	row := r.db.QueryRow(`
		SELECT uid, name, email, password_hash, phone, country_code, address, gender, user_type, is_helper_verified, fcm_token, created_at, discoverable_via_mutual_connections
		FROM users WHERE email = ?`, email)
	return scanUserWithHash(row)
}

func (r *Repository) FindByUID(uid string) (User, error) {
	row := r.db.QueryRow(`
		SELECT uid, name, email, password_hash, phone, country_code, address, gender, user_type, is_helper_verified, fcm_token, created_at, discoverable_via_mutual_connections
		FROM users WHERE uid = ?`, uid)
	u, _, err := scanUserWithHash(row)
	return u, err
}

func scanUserWithHash(row *sql.Row) (User, string, error) {
	var u User
	var hash, createdAt string
	err := row.Scan(&u.UID, &u.Name, &u.Email, &hash, &u.Phone, &u.CountryCode, &u.Address, &u.Gender, &u.UserType, &u.IsHelperVerified, &u.FCMToken, &createdAt, &u.DiscoverableViaMutualConnections)
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

// SetHelperVerified flips a helper's verified flag directly -- the one path
// besides internal/verification.Repository.Decide (an applicant's first
// approval) that can change it, used for the spec's §6 fast-track
// suspension: a credible report of a helper endangering a requester
// suspends them immediately, pending review, rather than waiting for the
// normal moderation queue.
func (r *Repository) SetHelperVerified(uid string, verified bool) error {
	res, err := r.db.Exec(`UPDATE users SET is_helper_verified = ? WHERE uid = ?`, verified, uid)
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
