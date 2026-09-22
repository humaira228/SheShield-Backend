// Package phone holds the one rule set for phone numbers, so signup, profile
// edits and trusted contacts can never disagree about what a valid number is.
package phone

import (
	"regexp"
	"strings"
)

var (
	countryCodeRe = regexp.MustCompile(`^\+[0-9]{1,4}$`)
	numberRe      = regexp.MustCompile(`^[0-9]{6,15}$`)
	separators    = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "")
)

// Normalize cleans and validates a phone number given as a country code
// ("+880") and a national number ("01712-345 678"). On success it returns the
// cleaned pair; otherwise it returns a user-facing message (shown as-is in
// the app) and the cleaned-so-far values are not meaningful.
//
// People type local numbers with a leading 0 (01712345678). In international
// format that 0 is dropped (+880 1712345678), which is the form an SMS
// gateway needs, so it is stored that way.
func Normalize(countryCode, number string) (cc, num, msg string) {
	cc = strings.TrimSpace(countryCode)
	num = separators.Replace(strings.TrimSpace(number))

	if !countryCodeRe.MatchString(cc) {
		return cc, num, "Country code must look like +880."
	}
	if !numberRe.MatchString(num) {
		return cc, num, "Enter a valid phone number (6-15 digits)."
	}
	return cc, strings.TrimPrefix(num, "0"), ""
}
