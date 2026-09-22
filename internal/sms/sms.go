// Package sms is the seam between SheShield and whatever actually delivers
// text messages. Everything else depends only on Sender, so adding a real
// provider later means adding one file here and one case in New -- nothing
// in the alert logic changes.
package sms

import (
	"context"
	"fmt"
	"log"
)

// Sender delivers one text message. Implementations must be safe for
// concurrent use, because alerts are sent to several contacts in parallel.
type Sender interface {
	// Send returns nil only if the provider accepted the message.
	Send(ctx context.Context, to, body string) error

	// Live reports whether Send really reaches a phone. The log-only sender
	// returns false, and the API then reports "simulated" instead of "sent" so
	// the app never tells someone help was notified when it was not.
	Live() bool
}

// LogSender prints messages instead of sending them. It is the default so the
// whole SOS flow can be developed and tested with no provider account and no
// cost. It writes phone numbers and locations to the log, so never use it in
// production.
type LogSender struct{}

func (LogSender) Send(_ context.Context, to, body string) error {
	log.Printf("[sms:log] would send to %s: %q", to, body)
	return nil
}

func (LogSender) Live() bool { return false }

// New picks a sender by name (the SMS_PROVIDER setting).
//
// To add a real provider: implement Sender in a new file in this package
// (an HTTP call to the provider's API), then add a case below.
func New(provider string) (Sender, error) {
	switch provider {
	case "", "log":
		return LogSender{}, nil
	default:
		return nil, fmt.Errorf("unknown SMS_PROVIDER %q (supported: log)", provider)
	}
}
