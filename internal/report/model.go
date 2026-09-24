package report

import "time"

// Review statuses a report moves through. There is no automated transition
// to 'actioned' anywhere in this package -- see the spec's bias safeguard
// (§5): only a human reviewer moves a report past 'pending'/'reviewing'.
const (
	StatusPending   = "pending"
	StatusReviewing = "reviewing"
	StatusActioned  = "actioned"
	StatusDismissed = "dismissed"
)

// ReporterRoleSystem marks a report the server itself filed (a rate-limit
// threshold crossing -- see internal/ratelimit) rather than a person. It
// enters the exact same queue a user-filed report does; there is no
// separate automated-action path.
const ReporterRoleSystem = "system"

type CreateReportRequest struct {
	ReportedID string `json:"reportedId"`
	Category   string `json:"category"`
	SOSID      string `json:"sosId,omitempty"`
}

type Report struct {
	ID           string     `json:"id"`
	ReporterID   string     `json:"reporterId"`
	ReportedID   string     `json:"reportedId"`
	ReporterRole string     `json:"reporterRole"`
	Category     string     `json:"category"`
	SOSID        string     `json:"sosId,omitempty"`
	ReviewStatus string     `json:"reviewStatus"`
	ReviewerID   string     `json:"reviewerId,omitempty"`
	Resolution   string     `json:"resolution,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	ReviewedAt   *time.Time `json:"reviewedAt,omitempty"`
}

type ReviewRequest struct {
	Status     string `json:"status"` // 'actioned' | 'dismissed'
	Resolution string `json:"resolution"`
	// MarkFalseSOS, when set alongside an 'actioned' review of a report tied
	// to an SOS, feeds that SOS's requester into sos_rate_limits'
	// false_count -- the one path into that counter (see internal/ratelimit).
	MarkFalseSOS bool `json:"markFalseSos"`
}

type Block struct {
	BlockerID string    `json:"blockerId"`
	BlockedID string    `json:"blockedId"`
	CreatedAt time.Time `json:"createdAt"`
}
