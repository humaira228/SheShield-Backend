package contact

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

var ErrNotFound = errors.New("contact not found")
var ErrInviteInvalid = errors.New("invite code is invalid or has expired")
var ErrCannotAcceptOwnInvite = errors.New("you cannot accept your own invite")

const inviteTTL = 7 * 24 * time.Hour

// codeAlphabet excludes visually-ambiguous characters (0/O, 1/I) since this
// code is meant to be read off one phone and typed into another.
const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func newInviteCode() string {
	raw := make([]byte, 8)
	_, _ = rand.Read(raw)
	code := make([]byte, 8)
	for i, v := range raw {
		code[i] = codeAlphabet[int(v)%len(codeAlphabet)]
	}
	return string(code)
}

func (r *Repository) ListForUser(userUID string) ([]Contact, error) {
	rows, err := r.db.Query(`
		SELECT id, name, relation, phone, country_code, created_at, linked_user_uid
		FROM trusted_contacts WHERE user_uid = ? ORDER BY created_at ASC, rowid ASC`, userUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	contacts := []Contact{}
	for rows.Next() {
		var c Contact
		var createdAt string
		if err := rows.Scan(&c.ID, &c.Name, &c.Relation, &c.Phone, &c.CountryCode, &createdAt, &c.LinkedUserUID); err != nil {
			return nil, err
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		contacts = append(contacts, c)
	}
	return contacts, rows.Err()
}

func (r *Repository) Create(userUID string, req CreateContactRequest) (Contact, error) {
	c := Contact{
		ID:          newID(),
		Name:        req.Name,
		Relation:    req.Relation,
		Phone:       req.Phone,
		CountryCode: req.CountryCode,
		CreatedAt:   time.Now().UTC(),
	}
	_, err := r.db.Exec(`
		INSERT INTO trusted_contacts (id, user_uid, name, relation, phone, country_code, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.ID, userUID, c.Name, c.Relation, c.Phone, c.CountryCode, c.CreatedAt.Format(time.RFC3339),
	)
	return c, err
}

func (r *Repository) Delete(userUID, id string) error {
	res, err := r.db.Exec(
		`DELETE FROM trusted_contacts WHERE id = ? AND user_uid = ?`, id, userUID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// AreConnected reports whether two accounts are linked via trusted_contacts
// in either direction -- one has the other's account as a linked trusted
// contact. This is the only real "social graph" this app has today, so it's
// the basis for the spec's §10 mutual-connection signal (see
// internal/helper.Service.NearbyAlerts): a simpler, direct-edge
// interpretation of "connection" rather than a full friend-of-friend graph,
// which this data model doesn't otherwise support.
func (r *Repository) AreConnected(uidA, uidB string) (bool, error) {
	var n int
	err := r.db.QueryRow(`
		SELECT COUNT(*) FROM trusted_contacts
		WHERE (user_uid = ? AND linked_user_uid = ?) OR (user_uid = ? AND linked_user_uid = ?)`,
		uidA, uidB, uidB, uidA,
	).Scan(&n)
	return n > 0, err
}

func (r *Repository) CountForUser(userUID string) (int, error) {
	var n int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM trusted_contacts WHERE user_uid = ?`, userUID,
	).Scan(&n)
	return n, err
}

// Exists reports whether this user already saved this exact number.
func (r *Repository) Exists(userUID, countryCode, phone string) (bool, error) {
	var n int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM trusted_contacts WHERE user_uid = ? AND country_code = ? AND phone = ?`,
		userUID, countryCode, phone,
	).Scan(&n)
	return n > 0, err
}

// CreateInvite issues a fresh invite code for userUID's contact, overwriting
// any earlier unused one (so re-sending the invite just gives a new code
// instead of stacking dead ones).
func (r *Repository) CreateInvite(userUID, contactID string) (InviteResponse, error) {
	resp := InviteResponse{
		Code:      newInviteCode(),
		ExpiresAt: time.Now().UTC().Add(inviteTTL),
	}
	res, err := r.db.Exec(
		`UPDATE trusted_contacts SET invite_code = ?, invite_expires_at = ?
		 WHERE id = ? AND user_uid = ?`,
		resp.Code, resp.ExpiresAt.Format(time.RFC3339), contactID, userUID,
	)
	if err != nil {
		return InviteResponse{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return InviteResponse{}, err
	}
	if n == 0 {
		return InviteResponse{}, ErrNotFound
	}
	return resp, nil
}

// AcceptInvite links acceptingUserUID's account to whichever contact record
// this code was issued for, then consumes the code so it can't be reused.
// Returns the linked contact's id and its owner's uid (the person who will
// now get an alarm push, not just an SMS, sent to acceptingUserUID).
func (r *Repository) AcceptInvite(code, acceptingUserUID string) (contactID, ownerUID string, err error) {
	var expiresAtStr string
	err = r.db.QueryRow(
		`SELECT id, user_uid, invite_expires_at FROM trusted_contacts WHERE invite_code = ?`, code,
	).Scan(&contactID, &ownerUID, &expiresAtStr)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrInviteInvalid
	}
	if err != nil {
		return "", "", err
	}
	expiresAt, _ := time.Parse(time.RFC3339, expiresAtStr)
	if time.Now().UTC().After(expiresAt) {
		return "", "", ErrInviteInvalid
	}
	if ownerUID == acceptingUserUID {
		return "", "", ErrCannotAcceptOwnInvite
	}
	if _, err := r.db.Exec(
		`UPDATE trusted_contacts SET linked_user_uid = ?, invite_code = NULL, invite_expires_at = NULL
		 WHERE id = ?`,
		acceptingUserUID, contactID,
	); err != nil {
		return "", "", err
	}
	return contactID, ownerUID, nil
}
