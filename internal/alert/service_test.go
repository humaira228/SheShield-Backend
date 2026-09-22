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
}

func (f *fakeStore) Save(a Alert) error {
	if f.err != nil {
		return f.err
	}
	f.saved = append(f.saved, a)
	return nil
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
	svc := NewService(fakeUsers{}, fakeContacts{}, &fakeStore{}, &fakeSender{live: true})
	if _, err := svc.Trigger(context.Background(), "u1", CreateAlertRequest{}); !errors.Is(err, ErrNoContacts) {
		t.Fatalf("got %v, want ErrNoContacts", err)
	}
}

func TestTrigger_BadLocation(t *testing.T) {
	svc := NewService(fakeUsers{}, contacts3(), &fakeStore{}, &fakeSender{live: true})
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
	svc := NewService(fakeUsers{}, contacts3(), store, sender)

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
	svc := NewService(fakeUsers{}, contacts3(), &fakeStore{}, sender)

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
	svc := NewService(fakeUsers{}, contacts3(), &fakeStore{}, &fakeSender{live: false})
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
	svc := NewService(fakeUsers{}, contacts3(), &fakeStore{err: errors.New("disk full")}, &fakeSender{live: true})
	a, err := svc.Trigger(context.Background(), "u1", CreateAlertRequest{})
	if err != nil {
		t.Fatalf("a DB error must not hide that texts went out, got %v", err)
	}
	if len(a.Deliveries) != 3 {
		t.Errorf("want 3 deliveries, got %d", len(a.Deliveries))
	}
}

func TestBuildMessage(t *testing.T) {
	if got := buildMessage("Zannat", f64(23.810332), f64(90.412518)); got !=
		"SheShield SOS: Zannat needs help. Location: https://maps.google.com/?q=23.810332,90.412518" {
		t.Errorf("unexpected message: %s", got)
	}
	if got := buildMessage("Zannat", nil, nil); !strings.Contains(got, "Location unavailable") {
		t.Errorf("unexpected message: %s", got)
	}
}
