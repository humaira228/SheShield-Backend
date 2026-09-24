package alert

import "time"

// Who actually delivered a message to a contact.
const (
	ChannelDevice = "device" // texted from the user's own SIM by the app
	ChannelServer = "server" // texted by this server through the SMS provider
)

// Outcome for one contact.
const (
	StatusSent      = "sent"
	StatusSimulated = "simulated" // server SMS is log-only: nothing really went out
	StatusFailed    = "failed"
)

type CreateAlertRequest struct {
	Latitude       *float64 `json:"latitude"`
	Longitude      *float64 `json:"longitude"`
	AccuracyMeters *float64 `json:"accuracyMeters"`

	// IDs of contacts the phone already texted successfully from its own SIM.
	// The server skips these, so nobody receives the same alert twice.
	NotifiedByDevice []string `json:"notifiedByDevice"`

	// AVConsent is the requester's real-time answer to "start audio/video
	// recording for this emergency?" -- never pre-checked client-side. Actual
	// recording capture/storage isn't implemented yet (needs legal sign-off
	// on two-party consent law first -- see the spec's §7); this only
	// persists the consent choice itself.
	AVConsent bool `json:"avConsent"`
}

type Delivery struct {
	ContactID string `json:"contactId"`
	Name      string `json:"name"`
	Phone     string `json:"-"` // stored for the record, never echoed back
	Channel   string `json:"channel"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

type Alert struct {
	ID             string     `json:"id"`
	UserUID        string     `json:"-"`
	Latitude       *float64   `json:"-"`
	Longitude      *float64   `json:"-"`
	AccuracyMeters *float64   `json:"-"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"-"`
	Deliveries     []Delivery `json:"deliveries"`

	// ShareToken/ShareURL point at the live-tracking page a contact can open
	// with no login (see internal/alert.Repository.NewShareToken and
	// Handler.trackingPage). Empty only if token generation failed -- that
	// must never stop the SOS itself from going out, so it's omitted from the
	// response rather than sent as a broken link.
	ShareToken string `json:"shareToken,omitempty"`
	ShareURL   string `json:"shareUrl,omitempty"`

	AVConsent bool `json:"avConsent"`
}

// UpdateLocationRequest is the PATCH /alerts/{id}/location body, sent
// periodically while an SOS is active -- the same shape as the location part
// of CreateAlertRequest, minus the device-dedup field that only makes sense
// at creation time.
type UpdateLocationRequest struct {
	Latitude       *float64 `json:"latitude"`
	Longitude      *float64 `json:"longitude"`
	AccuracyMeters *float64 `json:"accuracyMeters"`
}

// AlertSummary is one row of GET /api/v1/alerts (a user's own SOS history) --
// enough for a notification-style list (when, whether it's still active, how
// many contacts were reached) without the exact coordinates the full Alert
// type would carry. Deliberately its own type rather than reusing Alert: the
// two endpoints have different privacy/shape needs (see PublicAlertView for
// the same reasoning), and Alert's hidden fields (UserUID, Latitude, ...)
// exist for the create-response case, not this one.
type AlertSummary struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"createdAt"`
	ResolvedAt  *time.Time `json:"resolvedAt,omitempty"`
	SentCount   int        `json:"sentCount"`
	FailedCount int        `json:"failedCount"`
	TotalCount  int        `json:"totalCount"`
}

// PublicAlertView is everything the no-login tracking page may show a
// contact: enough to render a live map and know the SOS is still ongoing,
// nothing that could identify the sender beyond a first name -- no phone
// number, email, or full name (see internal/alert.Repository.GetByShareToken).
type PublicAlertView struct {
	Latitude       *float64  `json:"latitude"`
	Longitude      *float64  `json:"longitude"`
	AccuracyMeters *float64  `json:"accuracyMeters"`
	Status         string    `json:"status"`
	UpdatedAt      time.Time `json:"updatedAt"`
	FirstName      string    `json:"firstName"`

	// DuressActive: at least one duress signal (see internal/duress) has
	// fired on this SOS. Shown to anyone already on it -- trusted contacts,
	// the accepted helper -- per the spec's §3 disclosure table.
	DuressActive bool `json:"duressActive"`

	// ConnectivityLost: the requester hasn't sent a location update in over
	// connectivityLostAfter while the SOS is still 'accepted' -- "went quiet
	// during an emergency" is its own signal, distinct from a routine missed
	// check-in (see the spec's §8). Computed live from UpdatedAt, not stored.
	ConnectivityLost bool `json:"connectivityLost"`
}
