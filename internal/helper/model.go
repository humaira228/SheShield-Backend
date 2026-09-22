// Package helper implements helper-mode: a verified helper opts in with a
// location and radius, sees nearby SOS alerts (rough distance only), and can
// accept one to reveal its exact location and the requester's phone number.
// Follows the same handler -> service -> repository shape as
// internal/verification.
package helper

import "time"

// DefaultRadiusKm matches the Flutter HelperStatus default, so a helper who
// has never set a status yet sees the same radius on both sides.
const DefaultRadiusKm = 3.0

// Status is a helper's current availability. Field names/JSON tags mirror
// Flutter's HelperStatus exactly (isActive, radiusKm) so the datasource can
// stay a thin pass-through.
type Status struct {
	IsActive  bool      `json:"isActive"`
	RadiusKm  float64   `json:"radiusKm"`
	Latitude  *float64  `json:"-"`
	Longitude *float64  `json:"-"`
	UpdatedAt time.Time `json:"-"`
}

// SetStatusRequest is the PUT /helper/status body.
type SetStatusRequest struct {
	IsActive  bool     `json:"isActive"`
	RadiusKm  float64  `json:"radiusKm"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

// NearbyAlert is what an available helper sees before responding: a rough
// area and distance only. Mirrors Flutter's NearbyAlert (id, roughArea,
// distanceMeters, createdAt) -- the entity there structurally cannot carry
// precise coordinates, and this is why: the server never sends them.
type NearbyAlert struct {
	ID             string    `json:"id"`
	RoughArea      string    `json:"roughArea"`
	DistanceMeters float64   `json:"distanceMeters"`
	CreatedAt      time.Time `json:"createdAt"`
}

// AcceptedAlert is the full detail, returned only to the one helper who wins
// the accept race. Mirrors Flutter's AcceptedAlert exactly.
type AcceptedAlert struct {
	ID          string    `json:"id"`
	UserName    string    `json:"userName"`
	Phone       string    `json:"phone"`
	CountryCode string    `json:"countryCode"`
	Latitude    float64   `json:"latitude"`
	Longitude   float64   `json:"longitude"`
	AcceptedAt  time.Time `json:"acceptedAt"`
}

// activeAlert is one row read back from the alerts table for the nearby
// scan -- everything the service needs to compute distance and freshness,
// nothing more.
type activeAlert struct {
	ID        string
	UserUID   string
	Latitude  *float64
	Longitude *float64
	CreatedAt time.Time
}
