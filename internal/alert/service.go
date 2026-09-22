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

type Store interface {
	Save(a Alert) error
}

type Service struct {
	users    Users
	contacts Contacts
	store    Store
	sender   sms.Sender
}

func NewService(users Users, contacts Contacts, store Store, sender sms.Sender) *Service {
	return &Service{users: users, contacts: contacts, store: store, sender: sender}
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

	body := buildMessage(user.Name, req.Latitude, req.Longitude)
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

	alert := Alert{
		ID:             newID(),
		UserUID:        uid,
		Latitude:       req.Latitude,
		Longitude:      req.Longitude,
		AccuracyMeters: req.AccuracyMeters,
		CreatedAt:      time.Now().UTC(),
		Deliveries:     deliveries,
	}

	// The texts have already gone out, so a database problem must not hide
	// that from the caller. Log it and report the truth.
	if err := s.store.Save(alert); err != nil {
		log.Printf("alert: could not save alert %s: %v", alert.ID, err)
	}
	return alert, nil
}
