package auth

import (
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/zannatulmaliha/sheshield-backend/internal/middleware"
	"github.com/zannatulmaliha/sheshield-backend/internal/phone"
)

var ErrInvalidCredentials = errors.New("Incorrect email or password.")
var ErrWeakPassword = errors.New("Password must be at least 6 characters.")
var ErrNameRequired = errors.New("Name is required.")
var ErrInvalidEmail = errors.New("That email address looks invalid.")
var ErrInvalidGender = errors.New("Gender must be one of: female, male, other, preferNotToSay.")
var ErrInvalidUserType = errors.New("userType must be one of: user, helper, user_helper.")
var ErrRoleNotAllowed = errors.New("Only female accounts may sign up as a user. Other genders can only sign up as a helper.")

type Service struct {
	repo      *Repository
	jwtSecret string
	jwtTTL    int
}

func NewService(repo *Repository, jwtSecret string, jwtTTLHours int) *Service {
	return &Service{repo: repo, jwtSecret: jwtSecret, jwtTTL: jwtTTLHours}
}

func (s *Service) SignUp(req SignUpRequest) (AuthResponse, error) {
	if strings.TrimSpace(req.Name) == "" {
		return AuthResponse{}, ErrNameRequired
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
		return AuthResponse{}, ErrInvalidEmail
	}
	if len(req.Password) < 6 {
		return AuthResponse{}, ErrWeakPassword
	}
	if !validGenders[req.Gender] {
		return AuthResponse{}, ErrInvalidGender
	}
	if !validUserTypes[req.UserType] {
		return AuthResponse{}, ErrInvalidUserType
	}
	// Product rule: SheShield protects women. Only "female" accounts may be
	// a "user" or "user_helper" (both roles); every other gender may only
	// register as a "helper". Enforced here, not just in the Flutter UI,
	// since a client can always be bypassed.
	if req.Gender != "female" && req.UserType != "helper" {
		return AuthResponse{}, ErrRoleNotAllowed
	}

	countryCode, phoneNumber, msg := phone.Normalize(req.CountryCode, req.Phone)
	if msg != "" {
		return AuthResponse{}, errors.New(msg)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return AuthResponse{}, err
	}

	user := User{
		Name:        strings.TrimSpace(req.Name),
		Email:       email,
		Phone:       phoneNumber,
		CountryCode: countryCode,
		Gender:      req.Gender,
		UserType:    req.UserType,
	}
	created, err := s.repo.Create(user, string(hash))
	if err != nil {
		if errors.Is(err, ErrDuplicateEmail) {
			return AuthResponse{}, errors.New("An account with this email already exists.")
		}
		return AuthResponse{}, err
	}

	token, err := middleware.IssueToken(s.jwtSecret, created.UID, s.jwtTTL)
	if err != nil {
		return AuthResponse{}, err
	}
	return AuthResponse{User: created, Token: token}, nil
}

func (s *Service) SignIn(req SignInRequest) (AuthResponse, error) {
	email := strings.TrimSpace(strings.ToLower(req.Email))
	user, hash, err := s.repo.FindByEmail(email)
	if errors.Is(err, ErrNotFound) {
		return AuthResponse{}, ErrInvalidCredentials
	}
	if err != nil {
		return AuthResponse{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		return AuthResponse{}, ErrInvalidCredentials
	}

	token, err := middleware.IssueToken(s.jwtSecret, user.UID, s.jwtTTL)
	if err != nil {
		return AuthResponse{}, err
	}
	return AuthResponse{User: user, Token: token}, nil
}

func (s *Service) Me(uid string) (User, error) {
	return s.repo.FindByUID(uid)
}

// UpdateProfile applies the editable fields and returns the saved user.
// A ValidationError means the input was bad; any other error is a server fault.
func (s *Service) UpdateProfile(uid string, req UpdateProfileRequest) (User, error) {
	cur, err := s.repo.FindByUID(uid)
	if err != nil {
		return User{}, err
	}
	updated, msg := applyProfileUpdate(cur, req)
	if msg != "" {
		return User{}, ValidationError(msg)
	}
	if err := s.repo.UpdateProfile(updated); err != nil {
		return User{}, err
	}
	return updated, nil
}
