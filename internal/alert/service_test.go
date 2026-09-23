package alert

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/zannatulmaliha/sheshield-backend/internal/auth"
	"github.com/zannatulmaliha/sheshield-backend/internal/contact"
)

type fakeUsers struct{}

func (fakeUsers) FindByUID(string) (auth.User, error) { return auth.User{Name: "Zannat"}, nil }

type fakeContacts struct{ list []contact.Contact }

func (f fakeContacts) ListForUser(string) ([]contact.Contact, error) { return f.list, nil }

type fakeStore struct {
	saved []Alert
	err   error

	// shareToken is what NewShareToken returns; defaults to "tok123" so every
	// existing test using a bare &fakeStore{} keeps working unchanged.
	shareToken    string
	shareTokenErr error

	updateLocationErr error
	resolveErr        error

	publicView    *PublicAlertView
	publicViewErr error

	list    []AlertSummary
	listErr error
}

func (f *fakeStore) Save(a Alert) error {
	if f.err != nil {
		return f.err
	}
	f.saved = append(f.saved, a)
	return nil
}

func (f *fakeStore) NewShareToken() (string, error) {
	if f.shareTokenErr != nil {
		return "", f.shareTokenErr
	}
	if f.shareToken == "" {
		return "tok123", nil
	}
	return f.shareToken, nil
}

func (f *fakeStore) UpdateLocation(alertID, ownerUID string, lat, lng, accuracy *float64) error {
	return f.updateLocationErr
}

func (f *fakeStore) Resolve(alertID, ownerUID string) error {
	return f.resolveErr
}

func (f *fakeStore) GetByShareToken(token string) (*PublicAlertView, error) {
	if f.publicViewErr != nil {
		return nil, f.publicViewErr
	}
	return f.publicView, nil
}

func (f *fakeStore) ListByUser(uid string) ([]AlertSummary, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.list, nil
}

type fakeSender struct {
	mu       sync.Mutex
	live     bool
	failFor  map[string]bool
	sentTo   []string
	lastBody string
}

func (f *fakeSender) Send(_ context.Context, to, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sentTo = append(f.sentTo, to)
	f.lastBody = body
	if f.failFor[to] {
		return errors.New("provider down")
	}
	return nil
}
func (f *fakeSender) Live() bool { return f.live }

type fakePusher struct {
	mu   sync.Mutex
	sent []string
}

func (f *fakePusher) Send(_ context.Context, token, _ string, _, _ float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, token)
	return nil
}

func contacts3() fakeContacts {
	return fakeContacts{list: []contact.Contact{
		{ID: "c1", Name: "Mum", CountryCode: "+880", Phone: "1711111111"},
		{ID: "c2", Name: "Brother", CountryCode: "+880", Phone: "1722222222"},
		{ID: "c3", Name: "Friend", CountryCode: "+880", Phone: "1733333333"},
	}}
}

func f64(v float64) *float64 { return &v }

func byID(a Alert) map[string]Delivery {
	m := map[string]Delivery{}
	for _, d := range a.Deliveries {
		m[d.ContactID] = d
	}
	return m
}

func TestTrigger_NoContacts(t *testing.T) {
	svc := NewService(fakeUsers{}, fakeContacts{}, &fakeStore{}, &fakeSender{live: true}, &fakePusher{}, "http://localhost:8080")
	if _, err := svc.Trigger(context.Background(), "u1", CreateAlertRequest{}); !errors.Is(err, ErrNoContacts) {
		t.Fatalf("got %v, want ErrNoContacts", err)
	}
}

func TestTrigger_BadLocation(t *testing.T) {
	svc := NewService(fakeUsers{}, contacts3(), &fakeStore{}, &fakeSender{live: true}, &fakePusher{}, "http://localhost:8080")
	bad := []CreateAlertRequest{
		{Latitude: f64(23.8)},                       // lat without lng
		{Latitude: f64(91), Longitude: f64(90)},     // lat out of range
		{Latitude: f64(23.8), Longitude: f64(-181)}, // lng out of range
	}
	for i, req := range bad {
		if _, err := svc.Trigger(context.Background(), "u1", req); !errors.Is(err, ErrBadLocation) {
			t.Errorf("case %d: got %v, want ErrBadLocation", i, err)
		}
	}
}

func TestTrigger_SkipsContactsAlreadyTextedByPhone(t *testing.T) {
	sender := &fakeSender{live: true}
	store := &fakeStore{}
	svc := NewService(fakeUsers{}, contacts3(), store, sender, &fakePusher{}, "http://localhost:8080")

	a, err := svc.Trigger(context.Background(), "u1", CreateAlertRequest{
		Latitude: f64(23.81), Longitude: f64(90.41),
		NotifiedByDevice: []string{"c1", "not-my-contact"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(a.Deliveries) != 3 {
		t.Fatalf("want 3 deliveries (one per real contact), got %d", len(a.Deliveries))
	}
	got := byID(a)
	if d := got["c1"]; d.Channel != ChannelDevice || d.Status != StatusSent {
		t.Errorf("c1 = %+v, want device/sent", d)
	}
	for _, id := range []string{"c2", "c3"} {
		if d := got[id]; d.Channel != ChannelServer || d.Status != StatusSent {
			t.Errorf("%s = %+v, want server/sent", id, d)
		}
	}
	for _, to := range sender.sentTo {
		if to == "+8801711111111" {
			t.Error("server texted a contact the phone had already reached (duplicate)")
		}
	}
	if len(sender.sentTo) != 2 {
		t.Errorf("server should send exactly 2 texts, sent %d", len(sender.sentTo))
	}
	if len(store.saved) != 1 {
		t.Errorf("alert should be saved once, saved %d", len(store.saved))
	}
}

func TestTrigger_OneFailureDoesNotStopTheOthers(t *testing.T) {
	sender := &fakeSender{live: true, failFor: map[string]bool{"+8801722222222": true}}
	svc := NewService(fakeUsers{}, contacts3(), &fakeStore{}, sender, &fakePusher{}, "http://localhost:8080")

	a, err := svc.Trigger(context.Background(), "u1", CreateAlertRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := byID(a)
	if got["c1"].Status != StatusSent || got["c3"].Status != StatusSent {
		t.Errorf("healthy contacts should still be sent: %+v", got)
	}
	if got["c2"].Status != StatusFailed || got["c2"].Error == "" {
		t.Errorf("c2 should be failed with a reason: %+v", got["c2"])
	}
	if strings.Contains(got["c2"].Error, "provider down") {
		t.Error("provider internals must not leak to the client")
	}
}

func TestTrigger_LogOnlySenderReportsSimulatedNeverSent(t *testing.T) {
	svc := NewService(fakeUsers{}, contacts3(), &fakeStore{}, &fakeSender{live: false}, &fakePusher{}, "http://localhost:8080")
	a, err := svc.Trigger(context.Background(), "u1", CreateAlertRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range a.Deliveries {
		if d.Status != StatusSimulated {
			t.Errorf("%s = %q, want simulated (nothing was really sent)", d.ContactID, d.Status)
		}
	}
}

func TestTrigger_SaveFailureStillReportsWhatWasSent(t *testing.T) {
	svc := NewService(fakeUsers{}, contacts3(), &fakeStore{err: errors.New("disk full")}, &fakeSender{live: true}, &fakePusher{}, "http://localhost:8080")
	a, err := svc.Trigger(context.Background(), "u1", CreateAlertRequest{})
	if err != nil {
		t.Fatalf("a DB error must not hide that texts went out, got %v", err)
	}
	if len(a.Deliveries) != 3 {
		t.Errorf("want 3 deliveries, got %d", len(a.Deliveries))
	}
}

func TestBuildMessage(t *testing.T) {
	if got := buildMessage("Zannat", f64(23.810332), f64(90.412518), "http://localhost:8080/track/tok123"); got !=
		"SheShield SOS: Zannat needs help. Location: https://maps.google.com/?q=23.810332,90.412518 Track live: http://localhost:8080/track/tok123" {
		t.Errorf("unexpected message: %s", got)
	}
	if got := buildMessage("Zannat", nil, nil, "http://localhost:8080/track/tok123"); !strings.Contains(got, "Location unavailable") {
		t.Errorf("unexpected message: %s", got)
	}
	// No tracking link at all (e.g. token generation failed) must not leave a
	// dangling "Track live: " with nothing after it.
	if got := buildMessage("Zannat", nil, nil, ""); strings.Contains(got, "Track live") {
		t.Errorf("message should omit the tracking segment entirely when trackingURL is empty: %s", got)
	}
}

func TestFirstName(t *testing.T) {
	cases := map[string]string{
		"Zannatul Maliha": "Zannatul",
		"Rahim":           "Rahim",
		"":                "",
		"   Nadia  Islam": "Nadia",
	}
	for in, want := range cases {
		if got := firstName(in); got != want {
			t.Errorf("firstName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTrigger_MessageIncludesTrackingLink(t *testing.T) {
	sender := &fakeSender{live: true}
	store := &fakeStore{shareToken: "abc123"}
	svc := NewService(fakeUsers{}, contacts3(), store, sender, &fakePusher{}, "http://localhost:8080")

	a, err := svc.Trigger(context.Background(), "u1", CreateAlertRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if a.ShareToken != "abc123" || a.ShareURL != "http://localhost:8080/track/abc123" {
		t.Errorf("unexpected share fields: token=%q url=%q", a.ShareToken, a.ShareURL)
	}
	if !strings.Contains(sender.lastBody, "http://localhost:8080/track/abc123") {
		t.Errorf("SMS body should include the tracking link, got: %s", sender.lastBody)
	}
}

func TestTrigger_ShareTokenFailureStillSendsSOS(t *testing.T) {
	sender := &fakeSender{live: true}
	store := &fakeStore{shareTokenErr: errors.New("db down")}
	svc := NewService(fakeUsers{}, contacts3(), store, sender, &fakePusher{}, "http://localhost:8080")

	a, err := svc.Trigger(context.Background(), "u1", CreateAlertRequest{})
	if err != nil {
		t.Fatalf("a broken share-token generator must not block the SOS itself, got %v", err)
	}
	if len(sender.sentTo) != 3 {
		t.Errorf("want all 3 contacts still texted, got %d", len(sender.sentTo))
	}
	if a.ShareToken != "" || a.ShareURL != "" {
		t.Errorf("expected empty share fields on failure, got token=%q url=%q", a.ShareToken, a.ShareURL)
	}
	if strings.Contains(sender.lastBody, "Track live") {
		t.Errorf("message must not reference a tracking link that was never generated: %s", sender.lastBody)
	}
}

func TestUpdateLocation_RejectsBadLocation(t *testing.T) {
	svc := NewService(fakeUsers{}, contacts3(), &fakeStore{}, &fakeSender{}, &fakePusher{}, "http://localhost:8080")
	if err := svc.UpdateLocation("a1", "u1", nil, f64(90), f64(5)); !errors.Is(err, ErrBadLocation) {
		t.Fatalf("want ErrBadLocation for missing latitude, got %v", err)
	}
	if err := svc.UpdateLocation("a1", "u1", f64(91), f64(90), nil); !errors.Is(err, ErrBadLocation) {
		t.Fatalf("want ErrBadLocation for out-of-range latitude, got %v", err)
	}
}

func TestUpdateLocation_PassesThroughStoreErrors(t *testing.T) {
	store := &fakeStore{updateLocationErr: ErrAlertNotActive}
	svc := NewService(fakeUsers{}, contacts3(), store, &fakeSender{}, &fakePusher{}, "http://localhost:8080")
	if err := svc.UpdateLocation("a1", "u1", f64(23.8), f64(90.4), nil); !errors.Is(err, ErrAlertNotActive) {
		t.Fatalf("want ErrAlertNotActive, got %v", err)
	}
}

func TestResolve_PassesThroughStoreErrors(t *testing.T) {
	store := &fakeStore{resolveErr: ErrNotFound}
	svc := NewService(fakeUsers{}, contacts3(), store, &fakeSender{}, &fakePusher{}, "http://localhost:8080")
	if err := svc.Resolve("a1", "u1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestPublicView_ReturnsStoreResult(t *testing.T) {
	want := &PublicAlertView{FirstName: "Zannat", Status: "active"}
	store := &fakeStore{publicView: want}
	svc := NewService(fakeUsers{}, contacts3(), store, &fakeSender{}, &fakePusher{}, "http://localhost:8080")

	got, err := svc.PublicView("tok123")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("PublicView should pass through the store's result unchanged, got %+v", got)
	}
}

func TestPublicView_NotFound(t *testing.T) {
	store := &fakeStore{publicViewErr: ErrNotFound}
	svc := NewService(fakeUsers{}, contacts3(), store, &fakeSender{}, &fakePusher{}, "http://localhost:8080")
	if _, err := svc.PublicView("garbage"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestListMine_ReturnsStoreResult(t *testing.T) {
	want := []AlertSummary{{ID: "a1", Status: "resolved"}}
	store := &fakeStore{list: want}
	svc := NewService(fakeUsers{}, contacts3(), store, &fakeSender{}, &fakePusher{}, "http://localhost:8080")

	got, err := svc.ListMine("victim1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "a1" {
		t.Errorf("ListMine should pass through the store's result unchanged, got %+v", got)
	}
}

func TestListMine_PassesThroughStoreErrors(t *testing.T) {
	store := &fakeStore{listErr: errors.New("db down")}
	svc := NewService(fakeUsers{}, contacts3(), store, &fakeSender{}, &fakePusher{}, "http://localhost:8080")
	if _, err := svc.ListMine("victim1"); err == nil {
		t.Fatal("want an error when the store fails")
	}
}
