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
}
