package verification

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zannatulmaliha/sheshield-backend/internal/auth"
)

func testHandler(t *testing.T, user auth.User, store *fakeStore) *Handler {
	t.Helper()
	h := NewHandler(NewService(fakeUsers{user}, store, t.TempDir()))
	h.uid = func(*http.Request) (string, bool) { return "u1", true }
	return h
}

// upload builds a multipart request. A nil value leaves that field out.
func upload(t *testing.T, fields map[string][]byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for name, data := range fields {
		if data == nil {
			continue
		}
		part, err := mw.CreateFormFile(name, name+".jpg")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(data)
	}
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/verification", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}

func goodFields(t *testing.T) map[string][]byte {
	return map[string][]byte{
		"nidFront": pngBytes(t, 400, 300),
		"nidBack":  jpegBytes(t, 400, 300),
		"selfie":   jpegBytes(t, 300, 400),
	}
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad json %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestSubmitEndpoint_Success(t *testing.T) {
	store := &fakeStore{}
	h := testHandler(t, helper(), store)
	rec := httptest.NewRecorder()
	h.submit(rec, upload(t, goodFields(t)))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if store.created != 1 {
		t.Errorf("expected one submission, got %d", store.created)
	}

	// The response must never reveal where identity documents are stored.
	sub := store.subs[0]
	for _, secret := range []string{sub.ID, sub.NIDFront, sub.NIDBack, sub.Selfie, "nid_", "selfie."} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("response leaks %q: %s", secret, rec.Body.String())
		}
	}
	data, _ := decode(t, rec)["data"].(map[string]any)
	if data["status"] != StatusPending {
		t.Errorf("got %v", data)
	}
}

func TestSubmitEndpoint_Refusals(t *testing.T) {
	tooBig := make([]byte, MaxImageBytes+10)

	tests := []struct {
		name   string
		user   func() auth.User
		store  *fakeStore
		mutate func(map[string][]byte)
		want   int
	}{
		{"missing selfie", helper, &fakeStore{}, func(m map[string][]byte) { m["selfie"] = nil }, http.StatusBadRequest},
		{"missing all photos", helper, &fakeStore{}, func(m map[string][]byte) { clear(m) }, http.StatusBadRequest},
		{"not an image", helper, &fakeStore{}, func(m map[string][]byte) { m["nidBack"] = []byte("hello") }, http.StatusBadRequest},
		{"one photo too big", helper, &fakeStore{}, func(m map[string][]byte) { m["nidFront"] = tooBig }, http.StatusBadRequest},
		{"not a helper", func() auth.User { u := helper(); u.UserType = "user"; return u }, &fakeStore{}, func(map[string][]byte) {}, http.StatusForbidden},
		{"already pending", helper, &fakeStore{subs: []Submission{{Status: StatusPending}}}, func(map[string][]byte) {}, http.StatusConflict},
		{"already verified", func() auth.User { u := helper(); u.IsHelperVerified = true; return u }, &fakeStore{}, func(map[string][]byte) {}, http.StatusConflict},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fields := goodFields(t)
			tc.mutate(fields)
			rec := httptest.NewRecorder()
			testHandler(t, tc.user(), tc.store).submit(rec, upload(t, fields))
			if rec.Code != tc.want {
				t.Errorf("status %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if msg, _ := decode(t, rec)["error"].(string); msg == "" {
				t.Errorf("a refusal must carry a message the app can show: %s", rec.Body.String())
			}
		})
	}
}

func TestSubmitEndpoint_HugeRequestIsCutOff(t *testing.T) {
	// Well over the request cap: must be refused, not buffered forever.
	fields := goodFields(t)
	fields["selfie"] = bytes.Repeat([]byte{1}, maxRequestBytes+1024)
	rec := httptest.NewRecorder()
	testHandler(t, helper(), &fakeStore{}).submit(rec, upload(t, fields))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStatusEndpoint(t *testing.T) {
	store := &fakeStore{subs: []Submission{{Status: StatusRejected, Note: "ID photo is blurry"}}}
	rec := httptest.NewRecorder()
	testHandler(t, helper(), store).status(rec, httptest.NewRequest(http.MethodGet, "/api/v1/verification", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	data, _ := decode(t, rec)["data"].(map[string]any)
	if data["status"] != StatusRejected || data["note"] != "ID photo is blurry" {
		t.Errorf("got %v", data)
	}
}
