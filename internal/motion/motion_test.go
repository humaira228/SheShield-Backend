package motion

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	lat, lng := 23.8, 90.4
	bad := 95.0
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-2 * time.Minute)
	future := now.Add(time.Hour)

	cases := []struct {
		name    string
		req     ReportRequest
		wantErr error
		wantAt  time.Time
	}{
		{"ok defaults response", ReportRequest{Type: TypeFall, Confidence: 0.9}, nil, now},
		{"unknown type", ReportRequest{Type: "teleport", Confidence: 0.5}, ErrInvalidType, now},
		{"confidence > 1", ReportRequest{Type: TypeFall, Confidence: 1.2}, ErrInvalidConfidence, now},
		{"confidence NaN", ReportRequest{Type: TypeFall, Confidence: math.NaN()}, ErrInvalidConfidence, now},
		{"bad response", ReportRequest{Type: TypeFall, Confidence: .5, UserResponse: "maybe"}, ErrInvalidResponse, now},
		{"lat without lng", ReportRequest{Type: TypeFall, Confidence: .5, Latitude: &lat}, ErrBadLocation, now},
		{"lat out of range", ReportRequest{Type: TypeFall, Confidence: .5, Latitude: &bad, Longitude: &lng}, ErrBadLocation, now},
		{"queued offline event kept", ReportRequest{Type: TypeSprint, Confidence: .5, OccurredAt: &recent}, nil, recent},
		{"ancient timestamp falls back", ReportRequest{Type: TypeSprint, Confidence: .5, OccurredAt: &old}, nil, now},
		{"future timestamp falls back", ReportRequest{Type: TypeSprint, Confidence: .5, OccurredAt: &future}, nil, now},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at, err := c.req.Validate(now)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if err == nil && !at.Equal(c.wantAt) {
				t.Errorf("occurred = %v, want %v", at, c.wantAt)
			}
		})
	}
}

func TestValidate_DefaultsUserResponse(t *testing.T) {
	r := ReportRequest{Type: TypeStruggle, Confidence: 1}
	if _, err := r.Validate(time.Now()); err != nil {
		t.Fatal(err)
	}
	if r.UserResponse != ResponseNone {
		t.Errorf("UserResponse = %q, want none", r.UserResponse)
	}
}
