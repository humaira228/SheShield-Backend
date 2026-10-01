// Package trigger names what fired an SOS and turns that into the two things
// a helper is allowed to see before accepting: a plain-language label and a
// coarse risk level. It deliberately carries no requester identity.
package trigger

import "strings"

const (
	Manual         = "manual"          // SOS button
	Voice          = "voice"           // on-device voice-distress phrase
	MotionFall     = "motion_fall"     // fall + no "I'm OK" response
	MotionSprint   = "motion_sprint"   // abrupt sprint, user asked for help / didn't answer
	MotionStruggle = "motion_struggle" // violent erratic motion
	MotionInactive = "motion_inactive" // unresponsive after a fall
	MissedCheckin  = "missed_checkin"  // silent check-in unanswered
)

const (
	RiskHigh   = "high"
	RiskMedium = "medium"
)

var known = map[string]bool{
	Manual: true, Voice: true, MotionFall: true, MotionSprint: true,
	MotionStruggle: true, MotionInactive: true, MissedCheckin: true,
}

// Normalize maps client input to a known trigger, defaulting to Manual so an
// unknown/old client can never create an alert with a junk value.
func Normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if known[s] {
		return s
	}
	return Manual
}

// Label is what a helper reads on the alert card.
func Label(t string) string {
	switch t {
	case Voice:
		return "Voice distress detected"
	case MotionFall:
		return "Possible fall detected"
	case MotionSprint:
		return "Sudden sprint detected"
	case MotionStruggle:
		return "Struggle detected"
	case MotionInactive:
		return "Unresponsive after a fall"
	case MissedCheckin:
		return "Missed safety check-in"
	default:
		return "SOS button pressed"
	}
}

// Risk: everything a person confirmed or that implies bodily danger is high;
// a sprint on its own is ambiguous (a runner and someone being chased look
// the same to an accelerometer), so it is medium unless a duress signal
// says otherwise.
func Risk(t string, duressActive bool) string {
	if duressActive {
		return RiskHigh
	}
	if t == MotionSprint {
		return RiskMedium
	}
	return RiskHigh
}
