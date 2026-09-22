// Package verification handles helper identity verification: a helper uploads
// ID photos and a selfie, the submission sits as "pending", and only the admin
// command can approve or reject it. The app can never mark anyone verified.
package verification

import "time"

const (
	StatusNone     = "none" // never submitted (only ever reported, never stored)
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"
)

// Submission is one verification attempt. The photo fields are file names
// inside <upload dir>/<ID>/; they are never sent to any client.
type Submission struct {
	ID         string
	UserUID    string
	Status     string
	NIDFront   string
	NIDBack    string
	Selfie     string
	Note       string
	CreatedAt  time.Time
	ReviewedAt *time.Time
}

// StatusResponse is all the app ever learns: where things stand, and the
// reviewer's reason if it was rejected.
type StatusResponse struct {
	Status      string     `json:"status"`
	Note        string     `json:"note,omitempty"`
	SubmittedAt *time.Time `json:"submittedAt,omitempty"`
}

// Files are the three raw photos of one submission.
type Files struct {
	NIDFront []byte
	NIDBack  []byte
	Selfie   []byte
}

// Review is what the admin sees for one submission.
type Review struct {
	Submission
	UserName  string
	UserEmail string
	UserPhone string // international, e.g. +8801712345678
	UserType  string
}
