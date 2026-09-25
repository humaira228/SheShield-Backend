package helper

import (
	"errors"
	"log"
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
	ErrNotYourMatch    = errors.New("You don't currently hold this SOS.")
)

// alertFreshness matches the 15-minute stale-location/stale-alert window.
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
	Release(alertID, helperUID string, now time.Time) error
	SafetyStatus(alertID, helperUID string) (duressActive, connectivityLost bool, err error)
}

// Matches is this package's hook into internal/matching's sos_matches table.
type Matches interface {
	UpsertPending(sosID, helperID string) error
	Lock(sosID, helperID string, now time.Time) error
	ReleaseOthers(sosID, winningHelperID string, now time.Time) error
	Release(sosID string, now time.Time) (string, error)
}

// DiscoverabilityStore and ConnectionStore back the mutual-connection signal.
type DiscoverabilityStore interface {
	IsDiscoverable(uid string) (bool, error)
}

type ConnectionStore interface {
	AreConnected(uidA, uidB string) (bool, error)
}

type Service struct {
	users        Users
	status       StatusStore
	alerts       AlertsStore
	matches      Matches
	discoverable DiscoverabilityStore
	connections  ConnectionStore
	now          func() time.Time
}

func NewService(
	users Users,
	status StatusStore,
	alerts AlertsStore,
	matches Matches,
) *Service {
	return &Service{
		users:   users,
		status:  status,
		alerts:  alerts,
		matches: matches,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

// WithMutualConnections wires the optional mutual-connection signal.
func (s *Service) WithMutualConnections(
	discoverable DiscoverabilityStore,
	connections ConnectionStore,
) *Service {
	s.discoverable = discoverable
	s.connections = connections
	return s
}

func isHelper(u auth.User) bool {
	return u.UserType == "helper" || u.UserType == "user_helper"
}

// Status returns the caller's saved helper status.
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

// SetStatus updates the caller's availability.
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
		IsActive:              req.IsActive,
		RadiusKm:              req.RadiusKm,
		Latitude:              req.Latitude,
		Longitude:             req.Longitude,
		UpdatedAt:             s.now(),
		MutualConnectionOptIn: req.MutualConnectionOptIn,
	}

	if err := s.status.SetStatus(uid, st); err != nil {
		return Status{}, err
	}

	return st, nil
}

// NearbyAlerts lists open SOS alerts within the helper's own radius,
// closest first.
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

	// The helper must be active and have a valid location.
	if !st.IsActive || st.Latitude == nil || st.Longitude == nil {
		return nil, ErrNotActive
	}

	now := s.now()

	// Do not match alerts against an old helper location.
	if !isFresh(st.UpdatedAt, now) {
		return nil, ErrNotActive
	}

	all, err := s.alerts.ActiveAlerts()
	if err != nil {
		return nil, err
	}

	radiusMeters := st.RadiusKm * 1000

	out := make([]NearbyAlert, 0, len(all))

	for _, a := range all {
		// Never show the helper's own SOS.
		if a.UserUID == uid {
			continue
		}

		// Ignore stale SOS alerts.
		if !isFresh(a.CreatedAt, now) {
			continue
		}

		// IMPORTANT:
		// An active alert may not have location coordinates.
		// Never dereference nil pointers here.
		if a.Latitude == nil || a.Longitude == nil {
			continue
		}

		dist := haversineMeters(
			*st.Latitude,
			*st.Longitude,
			*a.Latitude,
			*a.Longitude,
		)

		if dist > radiusMeters {
			continue
		}

		out = append(out, NearbyAlert{
			ID:               a.ID,
			RoughArea:        "Nearby",
			DistanceMeters:   dist,
			CreatedAt:        a.CreatedAt,
			MutualConnection: s.mutualConnection(st.MutualConnectionOptIn, uid, a.UserUID),
		})

		// Best-effort match tracking.
		// A failure here must never prevent the helper from seeing the SOS.
		if s.matches != nil {
			if err := s.matches.UpsertPending(a.ID, uid); err != nil {
				log.Printf(
					"helper: could not record pending match for %s/%s: %v",
					a.ID,
					uid,
					err,
				)
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].DistanceMeters < out[j].DistanceMeters
	})

	return out, nil
}

// Accept attempts to claim an alert.
// A nil, nil return means someone else already won the race.
func (s *Service) Accept(uid, alertID string) (*AcceptedAlert, error) {
	user, err := s.users.FindByUID(uid)
	if err != nil {
		return nil, err
	}

	if !isHelper(user) || !user.IsHelperVerified {
		return nil, ErrNotHelper
	}

	now := s.now()

	row, won, err := s.alerts.Accept(alertID, uid, now)
	if err != nil {
		return nil, err
	}

	if !won {
		return nil, nil
	}

	if s.matches != nil {
		if err := s.matches.Lock(alertID, uid, now); err != nil {
			log.Printf(
				"helper: could not lock match %s/%s: %v",
				alertID,
				uid,
				err,
			)
		}

		if err := s.matches.ReleaseOthers(alertID, uid, now); err != nil {
			log.Printf(
				"helper: could not release other matches for %s: %v",
				alertID,
				err,
			)
		}
	}

	return &AcceptedAlert{
		ID:           alertID,
		UserName:     row.UserName,
		Phone:        row.Phone,
		CountryCode:  row.CountryCode,
		Latitude:     row.Latitude,
		Longitude:    row.Longitude,
		AcceptedAt:   now,
		RequesterUID: row.UserUID,
	}, nil
}

// Release lets the currently accepted helper back out.
func (s *Service) Release(uid, alertID string) error {
	user, err := s.users.FindByUID(uid)
	if err != nil {
		return err
	}

	if !isHelper(user) {
		return ErrNotHelper
	}

	now := s.now()

	if err := s.alerts.Release(alertID, uid, now); err != nil {
		return err
	}

	if s.matches != nil {
		if _, err := s.matches.Release(alertID, now); err != nil {
			log.Printf(
				"helper: could not release match row for %s: %v",
				alertID,
				err,
			)
		}
	}

	return nil
}

// SafetyStatus lets the currently accepted helper poll for live
// duress/connectivity signals.
func (s *Service) SafetyStatus(
	uid,
	alertID string,
) (duressActive, connectivityLost bool, err error) {
	user, err := s.users.FindByUID(uid)
	if err != nil {
		return false, false, err
	}

	if !isHelper(user) {
		return false, false, ErrNotHelper
	}

	return s.alerts.SafetyStatus(alertID, uid)
}

// mutualConnection implements the double-opt-in connection signal.
func (s *Service) mutualConnection(
	helperOptedIn bool,
	helperUID,
	requesterUID string,
) bool {
	if !helperOptedIn ||
		s.discoverable == nil ||
		s.connections == nil {
		return false
	}

	discoverable, err := s.discoverable.IsDiscoverable(requesterUID)
	if err != nil || !discoverable {
		return false
	}

	connected, err := s.connections.AreConnected(
		helperUID,
		requesterUID,
	)

	return err == nil && connected
}

// isFresh reports whether a timestamp is within the allowed freshness window.
//
// Future timestamps are also rejected. This protects against malformed
// timestamps accidentally being treated as fresh indefinitely.
func isFresh(t, now time.Time) bool {
	age := now.Sub(t)

	return age >= 0 && age <= alertFreshness
}

// haversineMeters calculates the great-circle distance between two
// latitude/longitude coordinates in meters.
func haversineMeters(
	lat1,
	lng1,
	lat2,
	lng2 float64,
) float64 {
	const earthRadiusM = 6371000.0

	toRad := func(deg float64) float64 {
		return deg * math.Pi / 180
	}

	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*
			math.Cos(toRad(lat2))*
			math.Sin(dLng/2)*
			math.Sin(dLng/2)

	// Protect against tiny floating-point errors causing
	// 1-a to become slightly negative.
	if a < 0 {
		a = 0
	}
	if a > 1 {
		a = 1
	}

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusM * c
}
