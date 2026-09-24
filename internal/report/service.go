package report

import (
	"errors"
	"time"

	"github.com/zannatulmaliha/sheshield-backend/internal/audit"
)

var (
	ErrInvalidCategory  = errors.New("A report category is required.")
	ErrCannotReportSelf = errors.New("You can't report yourself.")
	ErrNotFound         = errors.New("Report not found.")
	ErrAlreadyReviewed  = errors.New("This report was already reviewed.")
	ErrInvalidReview    = errors.New("Review status must be 'actioned' or 'dismissed'.")
)

// RateLimits is the one hook into internal/ratelimit this package needs:
// marking an SOS false when a report tied to it is actioned with
// MarkFalseSOS. Kept as a narrow interface (not a direct import) so report
// and ratelimit don't depend on each other's full APIs.
type RateLimits interface {
	MarkFalse(requesterUID string, now time.Time) error
}

// SOSOwners resolves an sos_id to its requester's uid, so MarkFalseSOS knows
// whose rate-limit counter to bump -- reports are filed against a person,
// but the counter belongs to whoever raised that specific SOS.
type SOSOwners interface {
	RequesterFor(sosID string) (string, error)
}

type Service struct {
	repo       *Repository
	log        *audit.Logger
	rateLimits RateLimits
	sosOwners  SOSOwners
	now        func() time.Time
}

func NewService(repo *Repository, log *audit.Logger, rateLimits RateLimits, sosOwners SOSOwners) *Service {
	return &Service{repo: repo, log: log, rateLimits: rateLimits, sosOwners: sosOwners, now: func() time.Time { return time.Now().UTC() }}
}

// File records a report from either direction (user-reports-helper or
// helper-reports-user) or the system (see ReporterRoleSystem) -- all three
// land in the identical queue.
func (s *Service) File(reporterID, reporterRole string, req CreateReportRequest) (Report, error) {
	if req.Category == "" {
		return Report{}, ErrInvalidCategory
	}
	if req.ReportedID == reporterID {
		return Report{}, ErrCannotReportSelf
	}

	rep, err := s.repo.Create(reporterID, req.ReportedID, reporterRole, req.Category, req.SOSID, s.now())
	if err != nil {
		return Report{}, err
	}
	_ = s.log.Log(reporterID, audit.ActionReportFiled, rep.ID)
	return rep, nil
}

// FileSystemFlag is the one path automated abuse signals (rate-limit
// threshold crossings) take into the moderation queue -- see the spec's
// bias safeguard: this only ever creates a 'pending' report for a human to
// look at, never an automatic restriction.
func (s *Service) FileSystemFlag(reportedID, category, sosID string) (Report, error) {
	rep, err := s.repo.Create("system", reportedID, ReporterRoleSystem, category, sosID, s.now())
	if err != nil {
		return Report{}, err
	}
	_ = s.log.Log("system", audit.ActionReportFiled, rep.ID)
	return rep, nil
}

// Queue returns the pending moderation queue -- one shared list regardless
// of who or what filed each report.
func (s *Service) Queue() ([]Report, error) {
	return s.repo.Queue(StatusPending)
}

// Get returns one report by id, mostly for a moderator's "show me the
// detail" view (see cmd/admin).
func (s *Service) Get(id string) (Report, error) {
	rep, err := s.repo.Get(id)
	if err != nil {
		return Report{}, ErrNotFound
	}
	return rep, nil
}

// Review is the only way a report leaves 'pending': a named human reviewer
// (reviewerID) marks it 'actioned' or 'dismissed' with a resolution note.
// If actioned with MarkFalseSOS set and the report is tied to an SOS, the
// requester's sos_rate_limits.false_count is bumped -- the one path into
// that counter, and even then it only ever produces a silent flag review
// gate (see internal/ratelimit), never an automatic restriction.
func (s *Service) Review(id, reviewerID string, req ReviewRequest) error {
	if req.Status != StatusActioned && req.Status != StatusDismissed {
		return ErrInvalidReview
	}
	rep, err := s.repo.Get(id)
	if err != nil {
		return ErrNotFound
	}
	if rep.ReviewStatus != StatusPending && rep.ReviewStatus != StatusReviewing {
		return ErrAlreadyReviewed
	}

	now := s.now()
	if err := s.repo.Review(id, reviewerID, req.Status, req.Resolution, now); err != nil {
		return err
	}
	_ = s.log.Log(reviewerID, audit.ActionReportReviewed, id)

	if req.Status == StatusActioned && req.MarkFalseSOS && rep.SOSID != "" && s.rateLimits != nil && s.sosOwners != nil {
		if requesterUID, err := s.sosOwners.RequesterFor(rep.SOSID); err == nil {
			_ = s.rateLimits.MarkFalse(requesterUID, now)
		}
	}
	return nil
}

func (s *Service) Block(blockerID, blockedID string) error {
	if blockerID == blockedID {
		return ErrCannotReportSelf
	}
	if err := s.repo.Block(blockerID, blockedID, s.now()); err != nil {
		return err
	}
	_ = s.log.Log(blockerID, audit.ActionBlockCreated, blockedID)
	return nil
}

func (s *Service) Unblock(blockerID, blockedID string) error {
	if err := s.repo.Unblock(blockerID, blockedID); err != nil {
		return err
	}
	_ = s.log.Log(blockerID, audit.ActionBlockRemoved, blockedID)
	return nil
}

func (s *Service) IsBlocked(userA, userB string) (bool, error) {
	return s.repo.IsBlocked(userA, userB)
}

func (s *Service) ListMyBlocks(blockerID string) ([]Block, error) {
	return s.repo.ListBlockedByMe(blockerID)
}
