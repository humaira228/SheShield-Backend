package helper

import (
	"errors"
	"testing"
	"time"

	"github.com/zannatulmaliha/sheshield-backend/internal/auth"
)

type fakeUsers struct{ user auth.User }

func (f fakeUsers) FindByUID(string) (auth.User, error) { return f.user, nil }

type fakeStatusStore struct {
	byUID map[string]Status
}

func newFakeStatusStore() *fakeStatusStore { return &fakeStatusStore{byUID: map[string]Status{}} }

func (f *fakeStatusStore) GetStatus(uid string) (Status, error) {
	if s, ok := f.byUID[uid]; ok {
		return s, nil
	}
	return Status{IsActive: false, RadiusKm: DefaultRadiusKm}, nil
}

func (f *fakeStatusStore) SetStatus(uid string, s Status) error {
	f.byUID[uid] = s
	return nil
}

type fakeAlertsStore struct {
	alerts     []activeAlert
	acceptRow  acceptedRow
	acceptWon  bool
	acceptErr  error
	acceptedID string // records what was passed in, for assertions
	releaseErr error

	safetyDuress   bool
	safetyConnLost bool
	safetyErr      error
}

func (f *fakeAlertsStore) ActiveAlerts() ([]activeAlert, error) { return f.alerts, nil }

func (f *fakeAlertsStore) Accept(alertID, helperUID string, now time.Time) (acceptedRow, bool, error) {
	f.acceptedID = alertID
	return f.acceptRow, f.acceptWon, f.acceptErr
}

func (f *fakeAlertsStore) Release(alertID, helperUID string, now time.Time) error {
	f.acceptedID = alertID
	return f.releaseErr
}

func (f *fakeAlertsStore) SafetyStatus(alertID, helperUID string) (bool, bool, error) {
	f.acceptedID = alertID
	return f.safetyDuress, f.safetyConnLost, f.safetyErr
}

func verifiedHelper() auth.User {
	return auth.User{UID: "h1", Name: "Rahim", UserType: "helper", IsHelperVerified: true}
}

func plainUser() auth.User {
	return auth.User{UID: "u1", Name: "Nadia", UserType: "user"}
}

func newTestService(user auth.User, status *fakeStatusStore, alerts *fakeAlertsStore, now time.Time) *Service {
	svc := NewService(fakeUsers{user}, status, alerts, nil)
	svc.now = func() time.Time { return now }
	return svc
}

func TestSetStatus_RejectsNonHelper(t *testing.T) {
	svc := newTestService(plainUser(), newFakeStatusStore(), &fakeAlertsStore{}, time.Now())
	_, err := svc.SetStatus("u1", SetStatusRequest{IsActive: false, RadiusKm: 3})
	if !errors.Is(err, ErrNotHelper) {
		t.Fatalf("want ErrNotHelper, got %v", err)
	}
}

func TestSetStatus_GoingActiveRequiresVerification(t *testing.T) {
	unverified := auth.User{UID: "h2", UserType: "helper", IsHelperVerified: false}
	lat, lng := 23.8, 90.4
	svc := newTestService(unverified, newFakeStatusStore(), &fakeAlertsStore{}, time.Now())
	_, err := svc.SetStatus("h2", SetStatusRequest{IsActive: true, RadiusKm: 3, Latitude: &lat, Longitude: &lng})
	if !errors.Is(err, ErrNotVerified) {
		t.Fatalf("want ErrNotVerified, got %v", err)
	}
}

func TestSetStatus_GoingActiveRequiresLocation(t *testing.T) {
	svc := newTestService(verifiedHelper(), newFakeStatusStore(), &fakeAlertsStore{}, time.Now())
	_, err := svc.SetStatus("h1", SetStatusRequest{IsActive: true, RadiusKm: 3})
	if !errors.Is(err, ErrLocationNeeded) {
		t.Fatalf("want ErrLocationNeeded, got %v", err)
	}
}

func TestSetStatus_RejectsRadiusOutOfBounds(t *testing.T) {
	svc := newTestService(verifiedHelper(), newFakeStatusStore(), &fakeAlertsStore{}, time.Now())
	_, err := svc.SetStatus("h1", SetStatusRequest{IsActive: false, RadiusKm: 100})
	if !errors.Is(err, ErrInvalidRadius) {
		t.Fatalf("want ErrInvalidRadius, got %v", err)
	}
}

func TestSetStatus_TurningOffNeverRequiresVerificationOrLocation(t *testing.T) {
	unverified := auth.User{UID: "h2", UserType: "helper", IsHelperVerified: false}
	svc := newTestService(unverified, newFakeStatusStore(), &fakeAlertsStore{}, time.Now())
	st, err := svc.SetStatus("h2", SetStatusRequest{IsActive: false, RadiusKm: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.IsActive || st.RadiusKm != 5 {
		t.Fatalf("unexpected status: %+v", st)
	}
}

func TestNearbyAlerts_RequiresActiveStatus(t *testing.T) {
	status := newFakeStatusStore() // no row => inactive by default
	svc := newTestService(verifiedHelper(), status, &fakeAlertsStore{}, time.Now())
	_, err := svc.NearbyAlerts("h1")
	if !errors.Is(err, ErrNotActive) {
		t.Fatalf("want ErrNotActive, got %v", err)
	}
}

func TestNearbyAlerts_FiltersByRadiusAndFreshnessAndSortsByDistance(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	helperLat, helperLng := 23.8103, 90.4125 // Dhaka
	status := newFakeStatusStore()
	status.byUID["h1"] = Status{
		IsActive: true, RadiusKm: 5, Latitude: &helperLat, Longitude: &helperLng, UpdatedAt: now,
	}

	near := 23.8110 // a few hundred meters away
	far := 24.5     // tens of km away, outside 5km radius
	alertsStore := &fakeAlertsStore{alerts: []activeAlert{
		{ID: "near", UserUID: "victim1", Latitude: &near, Longitude: &helperLng, CreatedAt: now},
		{ID: "far", UserUID: "victim2", Latitude: &far, Longitude: &helperLng, CreatedAt: now},
		{ID: "stale", UserUID: "victim3", Latitude: &near, Longitude: &helperLng, CreatedAt: now.Add(-30 * time.Minute)},
		{ID: "own", UserUID: "h1", Latitude: &near, Longitude: &helperLng, CreatedAt: now},
	}}

	svc := newTestService(verifiedHelper(), status, alertsStore, now)
	got, err := svc.NearbyAlerts("h1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ID != "near" {
		t.Fatalf("want exactly [near], got %+v", got)
	}
}

type fakeDiscoverability struct{ discoverable map[string]bool }

func (f fakeDiscoverability) IsDiscoverable(uid string) (bool, error) {
	return f.discoverable[uid], nil
}

type fakeConnections struct{ connected bool }

func (f fakeConnections) AreConnected(uidA, uidB string) (bool, error) { return f.connected, nil }

// TestNearbyAlerts_MutualConnectionRequiresBothSidesOptedIn is the §10
// double-opt-in property: the signal must be false unless the helper opted
// in (via Status.MutualConnectionOptIn), the requester opted in (via
// DiscoverabilityStore), AND they're actually connected -- any one being
// false/off must keep the whole thing false.
func TestNearbyAlerts_MutualConnectionRequiresBothSidesOptedIn(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	lat, lng := 23.8103, 90.4125

	newAlerts := func() *fakeAlertsStore {
		return &fakeAlertsStore{alerts: []activeAlert{
			{ID: "sos1", UserUID: "victim1", Latitude: &lat, Longitude: &lng, CreatedAt: now},
		}}
	}
	newStatus := func(optIn bool) *fakeStatusStore {
		s := newFakeStatusStore()
		s.byUID["h1"] = Status{IsActive: true, RadiusKm: 5, Latitude: &lat, Longitude: &lng, UpdatedAt: now, MutualConnectionOptIn: optIn}
		return s
	}

	cases := []struct {
		name         string
		helperOptIn  bool
		discoverable bool
		connected    bool
		want         bool
	}{
		{"neither opted in", false, false, true, false},
		{"only helper opted in", true, false, true, false},
		{"only requester opted in", false, true, true, false},
		{"both opted in but not connected", true, true, false, false},
		{"both opted in and connected", true, true, true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(verifiedHelper(), newStatus(tc.helperOptIn), newAlerts(), now)
			svc.WithMutualConnections(
				fakeDiscoverability{discoverable: map[string]bool{"victim1": tc.discoverable}},
				fakeConnections{connected: tc.connected},
			)
			got, err := svc.NearbyAlerts("h1")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("want 1 alert, got %d", len(got))
			}
			if got[0].MutualConnection != tc.want {
				t.Errorf("want MutualConnection=%v, got %v", tc.want, got[0].MutualConnection)
			}
		})
	}
}

func TestAccept_ReturnsNilOnLosingTheRace(t *testing.T) {
	alertsStore := &fakeAlertsStore{acceptWon: false}
	svc := newTestService(verifiedHelper(), newFakeStatusStore(), alertsStore, time.Now())
	got, err := svc.Accept("h1", "alert1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("want nil (lost the race), got %+v", got)
	}
}

func TestAccept_ReturnsFullDetailOnWinning(t *testing.T) {
	alertsStore := &fakeAlertsStore{
		acceptWon: true,
		acceptRow: acceptedRow{Latitude: 23.8, Longitude: 90.4, UserName: "Nadia", Phone: "1712345678", CountryCode: "+880"},
	}
	svc := newTestService(verifiedHelper(), newFakeStatusStore(), alertsStore, time.Now())
	got, err := svc.Accept("h1", "alert1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.UserName != "Nadia" || got.CountryCode != "+880" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestAccept_RejectsUnverifiedHelper(t *testing.T) {
	unverified := auth.User{UID: "h2", UserType: "helper", IsHelperVerified: false}
	svc := newTestService(unverified, newFakeStatusStore(), &fakeAlertsStore{}, time.Now())
	_, err := svc.Accept("h2", "alert1")
	if !errors.Is(err, ErrNotHelper) {
		t.Fatalf("want ErrNotHelper, got %v", err)
	}
}
