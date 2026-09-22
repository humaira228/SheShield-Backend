package helper

import (
	"errors"
	"math"
	"sort"
	"time"

	"github.com/zannatulmaliha/sheshield-backend/internal/auth"
)

var (
	ErrNotHelper       = errors.New("Only helper accounts can use helper mode.")
	ErrNotVerified     = errors.New("You need to complete helper verification before going active.")
	ErrLocationNeeded  = errors.New("Turning on helper mode requires your current location.")
	ErrInvalidRadius   = errors.New("Radius must be between 0.5 and 50 km.")
	ErrNotActive       = errors.New("Turn on helper mode to see nearby alerts.")
	ErrAlreadyAccepted = errors.New("Someone else already responded to this alert.")
)

// alertFreshness matches the 15-minute stale-location/stale-alert window
// mentioned in the handoff notes: an alert older than this no longer shows
// up as "nearby", the same way a helper's own last-known location goes
// stale after the same window (see isFresh below).
const alertFreshness = 15 * time.Minute

const minRadiusKm = 0.5
const maxRadiusKm = 50

// Small interfaces, so the service can be tested without a database.
type Users interface {
	FindByUID(uid string) (auth.User, error)
}

type StatusStore interface {
	GetStatus(uid string) (Status, error)
	SetStatus(uid string, s Status) error
}

type AlertsStore interface {
	ActiveAlerts() ([]activeAlert, error)
	Accept(alertID, helperUID string, now time.Time) (acceptedRow, bool, error)
}

type Service struct {
	users  Users
	status StatusStore
	alerts AlertsStore
	now    func() time.Time
}

func NewService(users Users, status StatusStore, alerts AlertsStore) *Service {
	return &Service{users: users, status: status, alerts: alerts, now: func() time.Time { return time.Now().UTC() }}
}

func isHelper(u auth.User) bool {
	return u.UserType == "helper" || u.UserType == "user_helper"
}

// Status returns the caller's saved helper status. Any account can read its
// own status (the dashboard needs this to decide which view to show), but
// only a verified helper can ever have IsActive true -- see SetStatus.
func (s *Service) Status(uid string) (Status, error) {
	user, err := s.users.FindByUID(uid)
	if err != nil {
		return Status{}, err
	}
	if !isHelper(user) {
		return Status{}, ErrNotHelper
	}
	return s.status.GetStatus(uid)
}

// SetStatus updates the caller's availability. Going active (isActive:
// true) requires a verified helper account and a current location; going
// inactive never requires either, so a helper can always turn themself off.
func (s *Service) SetStatus(uid string, req SetStatusRequest) (Status, error) {
	user, err := s.users.FindByUID(uid)
	if err != nil {
		return Status{}, err
	}
	if !isHelper(user) {
		return Status{}, ErrNotHelper
	}

	if req.RadiusKm < minRadiusKm || req.RadiusKm > maxRadiusKm {
		return Status{}, ErrInvalidRadius
	}

	if req.IsActive {
		if !user.IsHelperVerified {
			return Status{}, ErrNotVerified
		}
		if req.Latitude == nil || req.Longitude == nil {
			return Status{}, ErrLocationNeeded
		}
	}

	st := Status{
		IsActive:  req.IsActive,
		RadiusKm:  req.RadiusKm,
		Latitude:  req.Latitude,
		Longitude: req.Longitude,
		UpdatedAt: s.now(),
	}
	if err := s.status.SetStatus(uid, st); err != nil {
		return Status{}, err
	}
	return st, nil
}

// NearbyAlerts lists open SOS alerts within the helper's own radius of their
// last-set location, closest first. Requires the helper to currently be
// active with a location on file -- there is nothing to measure distance
// against otherwise.
func (s *Service) NearbyAlerts(uid string) ([]NearbyAlert, error) {
	user, err := s.users.FindByUID(uid)
	if err != nil {
		return nil, err
	}
	if !isHelper(user) || !user.IsHelperVerified {
		return nil, ErrNotHelper
	}

	st, err := s.status.GetStatus(uid)
	if err != nil {
		return nil, err
	}
	if !st.IsActive || st.Latitude == nil || st.Longitude == nil {
		return nil, ErrNotActive
	}
	if !isFresh(st.UpdatedAt, s.now()) {
		// The helper's own last-known location is stale -- treat that the
		// same as "not active" rather than matching alerts against a
		// position that may no longer be where they are.
		return nil, ErrNotActive
	}

	all, err := s.alerts.ActiveAlerts()
	if err != nil {
		return nil, err
	}

	radiusMeters := st.RadiusKm * 1000
	now := s.now()

	out := make([]NearbyAlert, 0, len(all))
	for _, a := range all {
		if a.UserUID == uid {
			continue // never show your own SOS back to yourself as "nearby"
		}
		if !isFresh(a.CreatedAt, now) {
			continue
		}
		dist := haversineMeters(*st.Latitude, *st.Longitude, *a.Latitude, *a.Longitude)
		if dist > radiusMeters {
			continue
		}
		out = append(out, NearbyAlert{
			ID:             a.ID,
			RoughArea:      "Nearby",
			DistanceMeters: dist,
			CreatedAt:      a.CreatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DistanceMeters < out[j].DistanceMeters })
	return out, nil
}

// Accept attempts to claim an alert. A nil, nil return means someone else
// already won the race -- the handler maps that to 409, not an error body.
func (s *Service) Accept(uid, alertID string) (*AcceptedAlert, error) {
	user, err := s.users.FindByUID(uid)
	if err != nil {
		return nil, err
	}
	if !isHelper(user) || !user.IsHelperVerified {
		return nil, ErrNotHelper
	}

	row, won, err := s.alerts.Accept(alertID, uid, s.now())
	if err != nil {
		return nil, err
	}
	if !won {
		return nil, nil
	}
	return &AcceptedAlert{
		ID:          alertID,
		UserName:    row.UserName,
		Phone:       row.Phone,
		CountryCode: row.CountryCode,
		Latitude:    row.Latitude,
		Longitude:   row.Longitude,
		AcceptedAt:  s.now(),
	}, nil
}

func isFresh(t, now time.Time) bool {
	return now.Sub(t) <= alertFreshness
}

// haversineMeters is the great-circle distance between two points, in
// meters -- accurate enough for the radius/proximity check here, which
// never needs to be more precise than "close enough to say 400m away".
func haversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusM = 6371000.0
	toRad := func(deg float64) float64 { return deg * math.Pi / 180 }

	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusM * c
}
