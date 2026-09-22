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
	Deliveries     []Delivery `json:"deliveries"`
}
