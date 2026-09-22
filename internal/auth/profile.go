package auth

import (
	"strings"
	"unicode/utf8"

	"github.com/zannatulmaliha/sheshield-backend/internal/phone"
)

// ValidationError is a message that is safe to show to the user as-is.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

// UpdateProfileRequest is deliberately small. Email, gender, role and the
// verified flag are NOT here: they can't be changed through this endpoint
// (the server rejects unknown fields), so a client can never promote itself.
// Every field is optional; only the ones sent are changed.
type UpdateProfileRequest struct {
	Name        *string `json:"name"`
	Phone       *string `json:"phone"`
	CountryCode *string `json:"countryCode"`
	Address     *string `json:"address"`
}

const (
	maxNameLen    = 60
	maxAddressLen = 200
)

// applyProfileUpdate returns cur with req applied, or a user-facing message
// if the result would be invalid. It touches no database, so it's easy to test.
func applyProfileUpdate(cur User, req UpdateProfileRequest) (User, string) {
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		switch {
		case name == "":
			return cur, "Name is required."
		case utf8.RuneCountInString(name) > maxNameLen:
			return cur, "Name is too long (60 characters max)."
		}
		cur.Name = name
	}

	if req.Phone != nil || req.CountryCode != nil {
		cc, num := cur.CountryCode, cur.Phone
		if req.CountryCode != nil {
			cc = *req.CountryCode
		}
		if req.Phone != nil {
			num = *req.Phone
		}
		cc, num, msg := phone.Normalize(cc, num)
		if msg != "" {
			return cur, msg
		}
		cur.CountryCode, cur.Phone = cc, num
	}

	if req.Address != nil {
		addr := strings.TrimSpace(*req.Address)
		if utf8.RuneCountInString(addr) > maxAddressLen {
			return cur, "Address is too long (200 characters max)."
		}
		cur.Address = addr // "" is allowed: it clears the address
	}

	return cur, ""
}
