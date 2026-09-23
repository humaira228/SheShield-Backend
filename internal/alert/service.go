package alert

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/zannatulmaliha/sheshield-backend/internal/auth"
	"github.com/zannatulmaliha/sheshield-backend/internal/contact"
	"github.com/zannatulmaliha/sheshield-backend/internal/sms"
)

var (
	ErrNoContacts  = errors.New("Add at least one trusted contact before sending an SOS.")
	ErrBadLocation = errors.New("Invalid location.")
)

// Small interfaces, so the service can be tested without a database.
type Users interface {
	FindByUID(uid string) (auth.User, error)
}

type Contacts interface {
	ListForUser(userUID string) ([]contact.Contact, error)
}

// Pusher alarms one linked trusted contact's phone. A separate, narrower
// interface than sms.Sender because it's fired off best-effort in the
// background (see Trigger) and never needs Live().
type Pusher interface {
	Send(ctx context.Context, token, senderName string, latitude, longitude float64) error
}

type Store interface {
	Save(a Alert) error
	NewShareToken() (string, error)
	UpdateLocation(alertID, ownerUID string, lat, lng, accuracy *float64) error
	Resolve(alertID, ownerUID string) error
	GetByShareToken(token string) (*PublicAlertView, error)
	ListByUser(uid string) ([]AlertSummary, error)
}

type Service struct {
	users    Users
	contacts Contacts
	store    Store
	sender   sms.Sender
	pusher   Pusher

	// baseURL prefixes the /track/<token> link put in the SOS text, e.g.
	// "https://api.sheshield.example". Comes from PUBLIC_BASE_URL.
	baseURL string
}

func NewService(users Users, contacts Contacts, store Store, sender sms.Sender, pusher Pusher, baseURL string) *Service {
	return &Service{users: users, contacts: contacts, store: store, sender: sender, pusher: pusher, baseURL: baseURL}
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Trigger sends the SOS to every contact the phone did not already reach, and
// records what happened. It returns an error only if nothing could be
// attempted (bad input, no contacts). A failed text to one contact is reported
// in the result, never as an error, so the app can show exactly who was and
// was not reached.
func (s *Service) Trigger(ctx context.Context, uid string, req CreateAlertRequest) (Alert, error) {
	if !validLocation(req.Latitude, req.Longitude) {
		return Alert{}, ErrBadLocation
	}

	user, err := s.users.FindByUID(uid)
	if err != nil {
		return Alert{}, err
	}
	contacts, err := s.contacts.ListForUser(uid)
	if err != nil {
		return Alert{}, err
	}
	if len(contacts) == 0 {
		return Alert{}, ErrNoContacts
	}

	// Only ids that really are this user's contacts matter; anything else in
	// the list is ignored because we only ever look up ids from `contacts`.
	byDevice := make(map[string]bool, len(req.NotifiedByDevice))
	for _, id := range req.NotifiedByDevice {
		byDevice[id] = true
	}

	// Generated up front (not inside Save) because the SMS body below needs
	// the tracking link before the alert is ever persisted. A failure here
	// must not stop the SOS itself -- see buildMessage's empty-trackingURL
	// case -- so it's logged, not returned.
	shareToken, err := s.store.NewShareToken()
	if err != nil {
		log.Printf("alert: could not generate share token: %v", err)
	}
	var trackingURL string
	if shareToken != "" {
		trackingURL = s.baseURL + "/track/" + shareToken
	}

	body := buildMessage(user.Name, req.Latitude, req.Longitude, trackingURL)
	deliveries := make([]Delivery, len(contacts))

	var wg sync.WaitGroup
	for i, c := range contacts {
		d := Delivery{ContactID: c.ID, Name: c.Name, Phone: c.CountryCode + c.Phone}

		if byDevice[c.ID] {
			d.Channel, d.Status = ChannelDevice, StatusSent
			deliveries[i] = d
			continue
		}

		d.Channel = ChannelServer
		wg.Add(1)
		go func(i int, d Delivery) {
			defer wg.Done()
			sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			if err := s.sender.Send(sendCtx, d.Phone, body); err != nil {
				log.Printf("alert: sms to contact %s failed: %v", d.ContactID, err)
				d.Status = StatusFailed
				d.Error = "Could not send the message."
			} else if s.sender.Live() {
				d.Status = StatusSent
			} else {
				d.Status = StatusSimulated
			}
			deliveries[i] = d
		}(i, d)
	}
	wg.Wait()

	// Alarm every contact who has linked their own SheShield account,
	// alongside (not instead of) the SMS everyone gets. Fire-and-forget: it
	// uses its own background context because the request's ctx is cancelled
	// as soon as this handler returns, and a slow/failed push must not delay
	// the SOS response or appear as a failed "delivery" -- there's no SMS
	// fallback-free path here, so it's genuinely best-effort.
	for _, c := range contacts {
		if c.LinkedUserUID == nil {
			continue
		}
		linkedUser, err := s.users.FindByUID(*c.LinkedUserUID)
		if err != nil || linkedUser.FCMToken == nil || *linkedUser.FCMToken == "" {
			continue
		}
		token := *linkedUser.FCMToken
		go func(token string) {
			pushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := s.pusher.Send(pushCtx, token, user.Name, *req.Latitude, *req.Longitude); err != nil {
				log.Printf("alert: push to linked contact failed: %v", err)
			}
		}(token)
	}

	now := time.Now().UTC()
	alert := Alert{
		ID:             newID(),
		UserUID:        uid,
		Latitude:       req.Latitude,
		Longitude:      req.Longitude,
		AccuracyMeters: req.AccuracyMeters,
		CreatedAt:      now,
		UpdatedAt:      now,
		ShareToken:     shareToken,
		ShareURL:       trackingURL,
		Deliveries:     deliveries,
	}

	// The texts have already gone out, so a database problem must not hide
	// that from the caller. Log it and report the truth.
	if err := s.store.Save(alert); err != nil {
		log.Printf("alert: could not save alert %s: %v", alert.ID, err)
	}
	return alert, nil
}

// UpdateLocation refreshes an in-progress SOS's last-known position. lat/lng
// are required (this endpoint exists to report a location, not clear one);
// ownership and the alert still being 'active' are enforced by the store,
// which returns ErrNotFound / ErrAlertNotActive as appropriate.
func (s *Service) UpdateLocation(alertID, ownerUID string, lat, lng, accuracy *float64) error {
	if lat == nil || lng == nil || !validLocation(lat, lng) {
		return ErrBadLocation
	}
	return s.store.UpdateLocation(alertID, ownerUID, lat, lng, accuracy)
}

// Resolve marks the caller's own alert 'resolved' -- they are safe now, so
// the tracking page should stop showing it as live.
func (s *Service) Resolve(alertID, ownerUID string) error {
	return s.store.Resolve(alertID, ownerUID)
}

// ListMine returns the caller's own SOS history, most recent first -- the
// notification-history list in the app.
func (s *Service) ListMine(uid string) ([]AlertSummary, error) {
	return s.store.ListByUser(uid)
}

// PublicView returns what the no-login tracking page may show for a share
// token. It deliberately takes no uid: the whole point of the token is that
// a contact who never signed in can still open the page.
func (s *Service) PublicView(token string) (*PublicAlertView, error) {
	return s.store.GetByShareToken(token)
}
