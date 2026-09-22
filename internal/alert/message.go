package alert

import "fmt"

// buildMessage is the text a contact receives. It is kept short: every extra
// 160 characters (only 70 for Bangla) is another billed segment once a paid
// provider is connected.
func buildMessage(name string, lat, lng *float64) string {
	if lat != nil && lng != nil {
		return fmt.Sprintf("SheShield SOS: %s needs help. Location: https://maps.google.com/?q=%.6f,%.6f", name, *lat, *lng)
	}
	return fmt.Sprintf("SheShield SOS: %s needs help. Location unavailable.", name)
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
