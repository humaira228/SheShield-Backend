package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zannatulmaliha/sheshield-backend/internal/httpx"
)

func sp(s string) *string { return &s }

func baseUser() User {
	return User{
		UID: "u1", Name: "Zannat", Email: "z@example.com",
		Phone: "1712345678", CountryCode: "+880", Address: "Uttara, Dhaka",
		Gender: "female", UserType: "user", IsHelperVerified: false,
	}
}

func TestApplyProfileUpdate_ChangesOnlyWhatWasSent(t *testing.T) {
	got, msg := applyProfileUpdate(baseUser(), UpdateProfileRequest{Name: sp("  Zannat M.  ")})
	if msg != "" {
		t.Fatal(msg)
	}
	want := baseUser()
	want.Name = "Zannat M."
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestApplyProfileUpdate_PhoneIsNormalisedLikeContacts(t *testing.T) {
	got, msg := applyProfileUpdate(baseUser(), UpdateProfileRequest{Phone: sp("01811-223 344"), CountryCode: sp("+880")})
	if msg != "" {
		t.Fatal(msg)
	}
	if got.Phone != "1811223344" || got.CountryCode != "+880" {
		t.Errorf("got phone=%q cc=%q", got.Phone, got.CountryCode)
	}
}

func TestApplyProfileUpdate_CountryCodeAloneKeepsNumber(t *testing.T) {
	got, msg := applyProfileUpdate(baseUser(), UpdateProfileRequest{CountryCode: sp("+91")})
	if msg != "" {
		t.Fatal(msg)
	}
	if got.CountryCode != "+91" || got.Phone != "1712345678" {
		t.Errorf("got %+v", got)
	}
}

func TestApplyProfileUpdate_AddressCanBeClearedButNotHuge(t *testing.T) {
	got, msg := applyProfileUpdate(baseUser(), UpdateProfileRequest{Address: sp("   ")})
	if msg != "" || got.Address != "" {
		t.Errorf("clearing failed: %q / %q", got.Address, msg)
	}
	if _, msg := applyProfileUpdate(baseUser(), UpdateProfileRequest{Address: sp(strings.Repeat("x", 201))}); msg == "" {
		t.Error("a 201-character address should be rejected")
	}
}

func TestApplyProfileUpdate_RejectsBadInput(t *testing.T) {
	bad := map[string]UpdateProfileRequest{
		"blank name":       {Name: sp("   ")},
		"name too long":    {Name: sp(strings.Repeat("a", 61))},
		"letters in phone": {Phone: sp("abc123456"), CountryCode: sp("+880")},
		"short phone":      {Phone: sp("12345"), CountryCode: sp("+880")},
		"bad country code": {Phone: sp("1712345678"), CountryCode: sp("880")},
	}
	for name, req := range bad {
		t.Run(name, func(t *testing.T) {
			got, msg := applyProfileUpdate(baseUser(), req)
			if msg == "" {
				t.Fatal("expected a validation message")
			}
			if got != baseUser() {
				t.Error("a rejected update must not change anything")
			}
		})
	}
}

func TestApplyProfileUpdate_EmptyRequestChangesNothing(t *testing.T) {
	got, msg := applyProfileUpdate(baseUser(), UpdateProfileRequest{})
	if msg != "" || got != baseUser() {
		t.Errorf("got %+v / %q", got, msg)
	}
}

// The safety-critical property: a client must not be able to change its own
// email, gender, role or verified flag through the profile endpoint. The
// server decodes the body strictly, so any such field is an error, not
// silently ignored.
func TestProfileEndpointRejectsPrivilegedFields(t *testing.T) {
	forbidden := []string{
		`{"isHelperVerified": true}`,
		`{"userType": "helper"}`,
		`{"gender": "female"}`,
		`{"email": "someone@else.com"}`,
		`{"uid": "another-user"}`,
		`{"name": "Ok", "isHelperVerified": true}`, // smuggled next to a legal field
	}
	for _, body := range forbidden {
		r := httptest.NewRequest(http.MethodPatch, "/api/v1/auth/me", strings.NewReader(body))
		var req UpdateProfileRequest
		if err := httpx.Decode(r, &req); err == nil {
			t.Errorf("body %s was accepted; it must be rejected", body)
		}
	}

	ok := httptest.NewRequest(http.MethodPatch, "/api/v1/auth/me",
		strings.NewReader(`{"name":"A","phone":"1712345678","countryCode":"+880","address":"x"}`))
	var req UpdateProfileRequest
	if err := httpx.Decode(ok, &req); err != nil {
		t.Errorf("a normal profile update was rejected: %v", err)
	}
}
