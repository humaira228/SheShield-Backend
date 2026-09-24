package auth

import "time"

// User's JSON shape is deliberately identical, field-for-field, to Flutter's
// AppUser.fromJson — that's what lets the Flutter data layer stay a thin
// pass-through instead of a translation layer.
type User struct {
	UID              string    `json:"uid"`
	Name             string    `json:"name"`
	Phone            string    `json:"phone"`
	CountryCode      string    `json:"countryCode"`
	Address          string    `json:"address"`
	Email            string    `json:"email"`
	Gender           string    `json:"gender"` // "female" | "male" | "other" | "preferNotToSay"
	UserType         string    `json:"userType"`
	IsHelperVerified bool      `json:"isHelperVerified"`
	FCMToken         *string   `json:"fcmToken,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`

	// DiscoverableViaMutualConnections is the requester-side half of the
	// §10 double opt-in -- see Service.SetDiscoverable. Read-only here;
	// only PATCH /api/v1/auth/discoverable can change it.
	DiscoverableViaMutualConnections bool `json:"discoverableViaMutualConnections"`
}

type SignUpRequest struct {
	Name        string `json:"name"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	Phone       string `json:"phone"`
	CountryCode string `json:"countryCode"`
	Gender      string `json:"gender"`
	// "user" | "helper" | "user_helper" — validated against Gender in
	// Service.SignUp: only "female" may pick "user" or "user_helper".
	UserType string `json:"userType"`

	// DeviceFingerprint is an opaque, app-generated device identifier --
	// purpose is multi-account abuse detection only (e.g. a banned user
	// re-registering). Stored but never echoed back in any response and
	// never joined into matching/behavior scoring; see
	// internal/auth.Service.DeviceFingerprint for the one, audit-logged
	// read path. Optional: an empty string is stored as-is, so older
	// clients that don't send one yet don't fail signup.
	DeviceFingerprint string `json:"deviceFingerprint,omitempty"`
}

type SignInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// UpdateFCMTokenRequest replaces the caller's push token wholesale -- there
// is only ever one live token per account (the most recent app install/
// reinstall), so partial updates make no sense here.
type UpdateFCMTokenRequest struct {
	Token string `json:"token"`
}

type AuthResponse struct {
	User  User   `json:"user"`
	Token string `json:"token"`
}

var validGenders = map[string]bool{
	"female": true, "male": true, "other": true, "preferNotToSay": true,
}

var validUserTypes = map[string]bool{
	"user": true, "helper": true, "user_helper": true,
}
