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
