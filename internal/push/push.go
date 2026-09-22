// Package push is the seam between SheShield and whatever actually wakes a
// linked trusted contact's phone to sound the in-app SOS alarm. Everything
// else depends only on Sender, so adding a real provider later means adding
// one file here and one case in New -- nothing in the alert logic changes.
// Mirrors internal/sms's shape deliberately: same problem (best-effort
// notify one recipient, log-only by default so nothing costs money or needs
// an account until you actually configure one), different transport.
package push

import (
	"context"
	"fmt"
	"log"
)

// Sender pushes one "sound the alarm" message to a device via its FCM
// registration token.
type Sender interface {
	// Send returns nil only if the provider accepted the push. lat/lng and
	// senderName let the receiving app jump straight to the alarm screen
	// with the SOS's details, without an extra API round trip.
	Send(ctx context.Context, token, senderName string, latitude, longitude float64) error

	// Live reports whether Send really reaches a device. The log-only
	// sender returns false, so callers never claim a contact was alarmed
	// when nothing was actually sent.
	Live() bool
}

// LogSender prints pushes instead of sending them. It is the default so the
// whole flow can be developed and tested with no Firebase project and no
// cost.
type LogSender struct{}

func (LogSender) Send(_ context.Context, token, senderName string, lat, lng float64) error {
	log.Printf("[push:log] would alarm token %s: %q needs help at %f,%f", token, senderName, lat, lng)
	return nil
}

func (LogSender) Live() bool { return false }

// New picks a sender by name (the PUSH_PROVIDER setting).
func New(provider, credentialsPath, projectID string) (Sender, error) {
	switch provider {
	case "", "log":
		return LogSender{}, nil
	case "fcm":
		return NewFCMSender(credentialsPath, projectID)
	default:
		return nil, fmt.Errorf("unknown PUSH_PROVIDER %q (supported: log, fcm)", provider)
	}
}
