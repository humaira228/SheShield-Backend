package alert

import (
	"fmt"
	"strings"
)

// buildMessage is the text a contact receives. It is kept short: every extra
// 160 characters (only 70 for Bangla) is another billed segment once a paid
// provider is connected. trackingURL is the /track/<token> live-tracking
// link (see Repository.NewShareToken); it is omitted from the text entirely
// rather than sent broken if token generation ever fails.
func buildMessage(name string, lat, lng *float64, trackingURL string) string {
	loc := "Location unavailable."
	if lat != nil && lng != nil {
		loc = fmt.Sprintf("Location: https://maps.google.com/?q=%.6f,%.6f", *lat, *lng)
	}
	if trackingURL == "" {
		return fmt.Sprintf("SheShield SOS: %s needs help. %s", name, loc)
	}
	return fmt.Sprintf("SheShield SOS: %s needs help. %s Track live: %s", name, loc, trackingURL)
}

// firstName returns just the first word of a full name, e.g. "Rahim" from
// "Rahim Uddin". Used only for the public tracking page, which must not show
// a contact anything more identifying about the sender than that.
func firstName(fullName string) string {
	fields := strings.Fields(fullName)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// validLocation accepts "no location" (both nil) or a real coordinate pair.
// One without the other, or out-of-range values, is a client bug.
func validLocation(lat, lng *float64) bool {
	if lat == nil && lng == nil {
		return true
	}
	if lat == nil || lng == nil {
		return false
	}
	return *lat >= -90 && *lat <= 90 && *lng >= -180 && *lng <= 180
}
