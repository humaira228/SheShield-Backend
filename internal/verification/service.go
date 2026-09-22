package verification

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/zannatulmaliha/sheshield-backend/internal/auth"
)

var (
	ErrNotHelper       = errors.New("Only helper accounts can be verified.")
	ErrAlreadyVerified = errors.New("You're already a verified helper.")
	ErrAlreadyPending  = errors.New("Your documents are already being reviewed.")
	ErrTooManyAttempts = errors.New("You've reached the limit of verification attempts. Please contact support.")
	ErrNotPending      = errors.New("That submission has already been decided.")
	ErrNotFound        = errors.New("submission not found")
)

// MaxAttempts caps total submissions per account, so a single account can't
// fill the disk with uploads.
const MaxAttempts = 5

// ImageError is a message about a bad photo, safe to show to the user as-is.
type ImageError string

func (e ImageError) Error() string { return string(e) }

// Small interfaces, so the service can be tested without a database.
type Users interface {
	FindByUID(uid string) (auth.User, error)
}

type Store interface {
	LatestForUser(uid string) (Submission, bool, error)
	CountForUser(uid string) (int, error)
	Create(s Submission) error // must return ErrAlreadyPending if one is already pending
}

type Service struct {
	users     Users
	store     Store
	uploadDir string
}

func NewService(users Users, store Store, uploadDir string) *Service {
	return &Service{users: users, store: store, uploadDir: uploadDir}
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Status is what the app is told. A verified account is always "approved",
// whatever the history says: the flag on the user is the source of truth.
func (s *Service) Status(uid string) (StatusResponse, error) {
	user, err := s.users.FindByUID(uid)
	if err != nil {
		return StatusResponse{}, err
	}
	if user.IsHelperVerified {
		return StatusResponse{Status: StatusApproved}, nil
	}
	latest, found, err := s.store.LatestForUser(uid)
	if err != nil {
		return StatusResponse{}, err
	}
	if !found {
		return StatusResponse{Status: StatusNone}, nil
	}
	resp := StatusResponse{Status: latest.Status, SubmittedAt: &latest.CreatedAt}
	if latest.Status == StatusRejected {
		resp.Note = latest.Note
	}
	return resp, nil
}

// Submit records a new verification attempt as "pending".
func (s *Service) Submit(uid string, f Files) (StatusResponse, error) {
	user, err := s.users.FindByUID(uid)
	if err != nil {
		return StatusResponse{}, err
	}
	if user.UserType != "helper" && user.UserType != "user_helper" {
		return StatusResponse{}, ErrNotHelper
	}
	if user.IsHelperVerified {
		return StatusResponse{}, ErrAlreadyVerified
	}

	count, err := s.store.CountForUser(uid)
	if err != nil {
		return StatusResponse{}, err
	}
	if count >= MaxAttempts {
		return StatusResponse{}, ErrTooManyAttempts
	}
	if latest, found, err := s.store.LatestForUser(uid); err != nil {
		return StatusResponse{}, err
	} else if found && latest.Status == StatusPending {
		return StatusResponse{}, ErrAlreadyPending
	}

	// Check all three before writing anything to disk.
	frontExt, problem := checkImage(f.NIDFront, "front of your ID")
	if problem != "" {
		return StatusResponse{}, ImageError(problem)
	}
	backExt, problem := checkImage(f.NIDBack, "back of your ID")
	if problem != "" {
		return StatusResponse{}, ImageError(problem)
	}
	selfieExt, problem := checkImage(f.Selfie, "selfie")
	if problem != "" {
		return StatusResponse{}, ImageError(problem)
	}

	sub := Submission{
		ID:        newID(),
		UserUID:   uid,
		Status:    StatusPending,
		NIDFront:  "nid_front" + frontExt,
		NIDBack:   "nid_back" + backExt,
		Selfie:    "selfie" + selfieExt,
		CreatedAt: time.Now().UTC(),
	}

	dir := filepath.Join(s.uploadDir, sub.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return StatusResponse{}, err
	}
	// If anything below fails, don't leave identity documents lying around
	// for a submission that doesn't exist.
	cleanup := func() {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("verification: could not remove %s: %v", dir, err)
		}
	}
	for name, data := range map[string][]byte{
		sub.NIDFront: f.NIDFront,
		sub.NIDBack:  f.NIDBack,
		sub.Selfie:   f.Selfie,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			cleanup()
			return StatusResponse{}, err
		}
	}

	if err := s.store.Create(sub); err != nil {
		cleanup()
		return StatusResponse{}, err
	}
	return StatusResponse{Status: StatusPending, SubmittedAt: &sub.CreatedAt}, nil
}
