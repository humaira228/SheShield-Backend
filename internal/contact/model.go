package contact

import "time"

type Contact struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Relation    string    `json:"relation"`
	Phone       string    `json:"phone"`
	CountryCode string    `json:"countryCode"`
	CreatedAt   time.Time `json:"createdAt"`
	// LinkedUserUID is set once this contact accepts an invite from their
	// own SheShield account (see AcceptInvite). Nil means they can only be
	// reached by SMS -- there is no app account to push an alarm to yet.
	LinkedUserUID *string `json:"linkedUserUid,omitempty"`
}

type CreateContactRequest struct {
	Name        string `json:"name"`
	Relation    string `json:"relation"`
	Phone       string `json:"phone"`
	CountryCode string `json:"countryCode"`
}

// InviteResponse is a short, human-typeable code (not a URL -- the app has
// no deep-link/App-Links setup) that the contact enters after installing
// SheShield and signing up, via AcceptInviteRequest.
type InviteResponse struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type AcceptInviteRequest struct {
	Code string `json:"code"`
}
