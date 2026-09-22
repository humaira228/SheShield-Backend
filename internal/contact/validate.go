package contact

import (
	"strings"
	"unicode/utf8"

	"github.com/zannatulmaliha/sheshield-backend/internal/phone"
)

// MaxContacts caps how many trusted contacts one account can have. An SOS
// alert will notify all of them, so an unbounded list is both a spam vector
// and (once SMS is wired up) an unbounded bill.
const MaxContacts = 10

// normalize cleans and validates a create request. It returns the cleaned
// request, or a user-facing message (shown as-is in the app) if it's invalid.
func normalize(req CreateContactRequest) (CreateContactRequest, string) {
	req.Name = strings.TrimSpace(req.Name)
	req.Relation = strings.TrimSpace(req.Relation)

	switch {
	case req.Name == "":
		return req, "Name is required."
	case utf8.RuneCountInString(req.Name) > 60:
		return req, "Name is too long (60 characters max)."
	case utf8.RuneCountInString(req.Relation) > 30:
		return req, "Relation is too long (30 characters max)."
	}

	cc, num, msg := phone.Normalize(req.CountryCode, req.Phone)
	if msg != "" {
		return req, msg
	}
	req.CountryCode, req.Phone = cc, num
	return req, ""
}
