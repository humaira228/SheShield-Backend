package verification

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zannatulmaliha/sheshield-backend/internal/auth"
)

type fakeUsers struct{ user auth.User }

func (f fakeUsers) FindByUID(string) (auth.User, error) { return f.user, nil }

type fakeStore struct {
	subs      []Submission
	createErr error
	created   int
}

func (f *fakeStore) LatestForUser(string) (Submission, bool, error) {
	if len(f.subs) == 0 {
		return Submission{}, false, nil
	}
	return f.subs[len(f.subs)-1], true, nil
}
func (f *fakeStore) CountForUser(string) (int, error) { return len(f.subs), nil }
func (f *fakeStore) Create(s Submission) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created++
	f.subs = append(f.subs, s)
	return nil
}

func helper() auth.User {
	return auth.User{UID: "u1", Name: "Rahim", UserType: "helper"}
}

func goodFiles(t *testing.T) Files {
	return Files{NIDFront: pngBytes(t, 400, 300), NIDBack: jpegBytes(t, 400, 300), Selfie: jpegBytes(t, 300, 400)}
}

func entries(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	e, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestSubmit_Success(t *testing.T) {
	dir := t.TempDir()
	store := &fakeStore{}
	svc := NewService(fakeUsers{helper()}, store, dir)

	resp, err := svc.Submit("u1", goodFiles(t))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != StatusPending || resp.SubmittedAt == nil {
		t.Errorf("got %+v", resp)
	}
	if store.created != 1 || store.subs[0].Status != StatusPending {
		t.Fatalf("expected one pending submission, got %+v", store.subs)
	}

	// The three photos are on disk, under the submission's own folder.
	sub := store.subs[0]
	for _, name := range []string{sub.NIDFront, sub.NIDBack, sub.Selfie} {
		if _, err := os.Stat(filepath.Join(dir, sub.ID, name)); err != nil {
			t.Errorf("missing stored photo %s: %v", name, err)
		}
	}
	if sub.NIDFront != "nid_front.png" || sub.NIDBack != "nid_back.jpg" || sub.Selfie != "selfie.jpg" {
		t.Errorf("stored names should come from the sniffed type, got %+v", sub)
	}
}

func TestSubmit_OnlyHelpersMayApply(t *testing.T) {
	for _, userType := range []string{"user", ""} {
		u := helper()
		u.UserType = userType
		svc := NewService(fakeUsers{u}, &fakeStore{}, t.TempDir())
		if _, err := svc.Submit("u1", goodFiles(t)); !errors.Is(err, ErrNotHelper) {
			t.Errorf("userType %q: got %v, want ErrNotHelper", userType, err)
		}
	}
	// Both roles that can respond to alerts are allowed.
	u := helper()
	u.UserType = "user_helper"
	if _, err := NewService(fakeUsers{u}, &fakeStore{}, t.TempDir()).Submit("u1", goodFiles(t)); err != nil {
		t.Errorf("user_helper should be allowed: %v", err)
	}
}

func TestSubmit_RefusedStates(t *testing.T) {
	t.Run("already verified", func(t *testing.T) {
		u := helper()
		u.IsHelperVerified = true
		svc := NewService(fakeUsers{u}, &fakeStore{}, t.TempDir())
		if _, err := svc.Submit("u1", goodFiles(t)); !errors.Is(err, ErrAlreadyVerified) {
			t.Errorf("got %v", err)
		}
	})
	t.Run("already pending", func(t *testing.T) {
		store := &fakeStore{subs: []Submission{{Status: StatusPending}}}
		svc := NewService(fakeUsers{helper()}, store, t.TempDir())
		if _, err := svc.Submit("u1", goodFiles(t)); !errors.Is(err, ErrAlreadyPending) {
			t.Errorf("got %v", err)
		}
	})
	t.Run("too many attempts", func(t *testing.T) {
		store := &fakeStore{}
		for i := 0; i < MaxAttempts; i++ {
			store.subs = append(store.subs, Submission{Status: StatusRejected})
		}
		svc := NewService(fakeUsers{helper()}, store, t.TempDir())
		if _, err := svc.Submit("u1", goodFiles(t)); !errors.Is(err, ErrTooManyAttempts) {
			t.Errorf("got %v", err)
		}
	})
}

func TestSubmit_RejectedHelperCanTryAgain(t *testing.T) {
	store := &fakeStore{subs: []Submission{{Status: StatusRejected, Note: "blurry"}}}
	svc := NewService(fakeUsers{helper()}, store, t.TempDir())
	if _, err := svc.Submit("u1", goodFiles(t)); err != nil {
		t.Fatalf("a rejected helper must be able to resubmit: %v", err)
	}
}

func TestSubmit_BadPhotoWritesNothing(t *testing.T) {
	dir := t.TempDir()
	f := goodFiles(t)
	f.Selfie = []byte("definitely not an image")
	svc := NewService(fakeUsers{helper()}, &fakeStore{}, dir)

	_, err := svc.Submit("u1", f)
	var imgErr ImageError
	if !errors.As(err, &imgErr) {
		t.Fatalf("got %v, want an ImageError", err)
	}
	if n := len(entries(t, dir)); n != 0 {
		t.Errorf("a refused upload must leave nothing on disk, found %d entries", n)
	}
}

func TestSubmit_FailedSaveLeavesNoIdentityDocumentsBehind(t *testing.T) {
	for name, createErr := range map[string]error{
		"database error":        errors.New("disk full"),
		"lost a race (pending)": ErrAlreadyPending,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			svc := NewService(fakeUsers{helper()}, &fakeStore{createErr: createErr}, dir)
			if _, err := svc.Submit("u1", goodFiles(t)); !errors.Is(err, createErr) {
				t.Fatalf("got %v, want %v", err, createErr)
			}
			if n := len(entries(t, dir)); n != 0 {
				t.Errorf("photos of a submission that doesn't exist were left on disk (%d entries)", n)
			}
		})
	}
}

func TestStatus(t *testing.T) {
	when := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)

	t.Run("none", func(t *testing.T) {
		got, err := NewService(fakeUsers{helper()}, &fakeStore{}, t.TempDir()).Status("u1")
		if err != nil || got.Status != StatusNone {
			t.Errorf("got %+v / %v", got, err)
		}
	})
	t.Run("pending hides nothing sensitive and has no note", func(t *testing.T) {
		store := &fakeStore{subs: []Submission{{Status: StatusPending, CreatedAt: when, Note: "internal"}}}
		got, _ := NewService(fakeUsers{helper()}, store, t.TempDir()).Status("u1")
		if got.Status != StatusPending || got.Note != "" || got.SubmittedAt == nil {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("rejected shows the reason", func(t *testing.T) {
		store := &fakeStore{subs: []Submission{{Status: StatusRejected, Note: "ID photo is blurry", CreatedAt: when}}}
		got, _ := NewService(fakeUsers{helper()}, store, t.TempDir()).Status("u1")
		if got.Status != StatusRejected || got.Note != "ID photo is blurry" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("the verified flag on the user wins over history", func(t *testing.T) {
		u := helper()
		u.IsHelperVerified = true
		store := &fakeStore{subs: []Submission{{Status: StatusRejected, Note: "old"}}}
		got, _ := NewService(fakeUsers{u}, store, t.TempDir()).Status("u1")
		if got.Status != StatusApproved || got.Note != "" {
			t.Errorf("got %+v", got)
		}
	})
}
