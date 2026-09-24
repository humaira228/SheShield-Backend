package report

import (
	"testing"
	"time"

	"github.com/zannatulmaliha/sheshield-backend/internal/audit"
)

type fakeRateLimits struct {
	markedFalseFor []string
}

func (f *fakeRateLimits) MarkFalse(userID string, now time.Time) error {
	f.markedFalseFor = append(f.markedFalseFor, userID)
	return nil
}

type fakeSOSOwners struct{ owner string }

func (f fakeSOSOwners) RequesterFor(sosID string) (string, error) { return f.owner, nil }

func newTestService(t *testing.T, rl RateLimits, owners SOSOwners) *Service {
	t.Helper()
	conn := openTestDB(t)
	return NewService(NewRepository(conn), audit.NewLogger(conn), rl, owners)
}

func TestFile_CannotReportSelf(t *testing.T) {
	svc := newTestService(t, nil, nil)
	_, err := svc.File("u1", "user", CreateReportRequest{ReportedID: "u1", Category: "harassment"})
	if err != ErrCannotReportSelf {
		t.Fatalf("want ErrCannotReportSelf, got %v", err)
	}
}

func TestFile_RequiresCategory(t *testing.T) {
	svc := newTestService(t, nil, nil)
	_, err := svc.File("u1", "user", CreateReportRequest{ReportedID: "u2"})
	if err != ErrInvalidCategory {
		t.Fatalf("want ErrInvalidCategory, got %v", err)
	}
}

// TestReview_DismissedNeverMarksFalse is the bias-safeguard property that
// matters most here: a reviewer dismissing a report must never touch the
// requester's rate-limit counters, whatever MarkFalseSOS says -- only an
// explicit 'actioned' review can.
func TestReview_DismissedNeverMarksFalse(t *testing.T) {
	rl := &fakeRateLimits{}
	svc := newTestService(t, rl, fakeSOSOwners{owner: "requester1"})

	rep, err := svc.File("h1", "helper", CreateReportRequest{ReportedID: "u2", Category: "false_alarm", SOSID: "sos1"})
	if err != nil {
		t.Fatalf("file: %v", err)
	}

	if err := svc.Review(rep.ID, "moderator1", ReviewRequest{Status: StatusDismissed, Resolution: "no evidence", MarkFalseSOS: true}); err != nil {
		t.Fatalf("review: %v", err)
	}
	if len(rl.markedFalseFor) != 0 {
		t.Fatalf("dismissing a report must never mark an SOS false, got %v", rl.markedFalseFor)
	}
}

// TestReview_ActionedWithMarkFalseSOSBumpsTheRequesterNotTheReporter checks
// the counter lands on the SOS's actual requester, not the person who
// happened to file the report -- those are different accounts (a helper
// reports a user's SOS as fake; the *user*, not the helper, is the one
// whose pattern this should track).
func TestReview_ActionedWithMarkFalseSOSBumpsTheRequesterNotTheReporter(t *testing.T) {
	rl := &fakeRateLimits{}
	svc := newTestService(t, rl, fakeSOSOwners{owner: "requester1"})

	rep, err := svc.File("h1", "helper", CreateReportRequest{ReportedID: "u2", Category: "false_alarm", SOSID: "sos1"})
	if err != nil {
		t.Fatalf("file: %v", err)
	}

	if err := svc.Review(rep.ID, "moderator1", ReviewRequest{Status: StatusActioned, Resolution: "confirmed false", MarkFalseSOS: true}); err != nil {
		t.Fatalf("review: %v", err)
	}
	if len(rl.markedFalseFor) != 1 || rl.markedFalseFor[0] != "requester1" {
		t.Fatalf("want requester1 (the SOS owner) marked false, got %v", rl.markedFalseFor)
	}
}

func TestReview_ActionedWithoutMarkFalseSOSDoesNotBumpCounter(t *testing.T) {
	rl := &fakeRateLimits{}
	svc := newTestService(t, rl, fakeSOSOwners{owner: "requester1"})

	rep, err := svc.File("h1", "helper", CreateReportRequest{ReportedID: "u2", Category: "rudeness", SOSID: "sos1"})
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if err := svc.Review(rep.ID, "moderator1", ReviewRequest{Status: StatusActioned, Resolution: "warned"}); err != nil {
		t.Fatalf("review: %v", err)
	}
	if len(rl.markedFalseFor) != 0 {
		t.Fatalf("MarkFalseSOS was false -- want no counter bump, got %v", rl.markedFalseFor)
	}
}

func TestReview_CannotReviewTwice(t *testing.T) {
	svc := newTestService(t, nil, nil)
	rep, err := svc.File("u1", "user", CreateReportRequest{ReportedID: "u2", Category: "harassment"})
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if err := svc.Review(rep.ID, "moderator1", ReviewRequest{Status: StatusDismissed, Resolution: "no evidence"}); err != nil {
		t.Fatalf("first review: %v", err)
	}
	if err := svc.Review(rep.ID, "moderator2", ReviewRequest{Status: StatusActioned, Resolution: "changed my mind"}); err != ErrAlreadyReviewed {
		t.Fatalf("want ErrAlreadyReviewed, got %v", err)
	}
}
