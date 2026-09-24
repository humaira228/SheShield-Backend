// Command admin reviews helper verification requests. It works directly on
// the database and the upload folder, so it can only be run by someone with
// access to the server's files -- there is deliberately no network endpoint
// for approving helpers.
//
//	go run ./cmd/admin pending
//	go run ./cmd/admin show <id>
//	go run ./cmd/admin approve <id>
//	go run ./cmd/admin reject <id> "reason shown to the applicant"
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zannatulmaliha/sheshield-backend/internal/config"
	"github.com/zannatulmaliha/sheshield-backend/internal/db"
	"github.com/zannatulmaliha/sheshield-backend/internal/verification"
)

const usage = `Usage:
  admin pending                 list helpers waiting for review
  admin show <id>               applicant details and where their photos are
  admin approve <id>            verify this helper
  admin reject <id> "reason"    reject; the applicant sees the reason and may resubmit
` + reportsUsage

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}

	cfg := config.Load()
	conn, err := db.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not open the database:", err)
		os.Exit(1)
	}
	defer conn.Close()
	repo := verification.NewRepository(conn)
	reportSvc := reportServiceFor(conn)

	switch os.Args[1] {
	case "pending":
		err = pending(repo)
	case "show":
		err = withID(os.Args, 3, func(id string) error { return show(repo, cfg.UploadDir, id) })
	case "approve":
		err = withID(os.Args, 3, func(id string) error { return decide(repo, id, true, "") })
	case "reject":
		err = withID(os.Args, 4, func(id string) error {
			reason := strings.TrimSpace(os.Args[3])
			if reason == "" {
				return errors.New(`a reason is required, e.g. reject <id> "ID photo is blurry"`)
			}
			return decide(repo, id, false, reason)
		})
	case "reports":
		err = dispatchReports(conn, reportSvc, os.Args[2:])
	case "suspend-helper":
		err = withID(os.Args, 4, func(uid string) error {
			reason := strings.TrimSpace(os.Args[3])
			if reason == "" {
				return errors.New(`a reason is required, e.g. suspend-helper <uid> "endangered requester during active SOS"`)
			}
			return suspendHelper(conn, uid, reason)
		})
	case "fingerprint":
		err = withID(os.Args, 4, func(uid string) error {
			reason := strings.TrimSpace(os.Args[3])
			if reason == "" {
				return errors.New(`a reason is required, e.g. fingerprint <uid> "checking for ban evasion"`)
			}
			return fingerprint(conn, uid, reason)
		})
	default:
		fmt.Print(usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func withID(args []string, wantArgs int, fn func(id string) error) error {
	if len(args) != wantArgs {
		fmt.Print(usage)
		os.Exit(2)
	}
	return fn(args[2])
}

func pending(repo *verification.Repository) error {
	list, err := repo.ListPending()
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("Nothing waiting for review.")
		return nil
	}
	fmt.Printf("%d waiting for review (oldest first):\n\n", len(list))
	for _, rv := range list {
		fmt.Printf("  %s   %s <%s>   submitted %s\n",
			rv.ID, rv.UserName, rv.UserEmail, rv.CreatedAt.Local().Format("2006-01-02 15:04"))
	}
	fmt.Println("\nLook at one with:  admin show <id>")
	return nil
}

func show(repo *verification.Repository, uploadDir, id string) error {
	rv, err := repo.GetForReview(id)
	if err != nil {
		return friendly(err)
	}
	dir, err := filepath.Abs(filepath.Join(uploadDir, rv.ID))
	if err != nil {
		return err
	}

	fmt.Printf("Submission  %s\n", rv.ID)
	fmt.Printf("Status      %s\n", rv.Status)
	fmt.Printf("Submitted   %s\n\n", rv.CreatedAt.Local().Format("2006-01-02 15:04"))
	fmt.Printf("Name        %s\n", rv.UserName)
	fmt.Printf("Email       %s\n", rv.UserEmail)
	fmt.Printf("Phone       %s\n", rv.UserPhone)
	fmt.Printf("Role        %s\n\n", rv.UserType)
	fmt.Printf("Photos (open these to review):\n")
	fmt.Printf("  ID front  %s\n", filepath.Join(dir, rv.NIDFront))
	fmt.Printf("  ID back   %s\n", filepath.Join(dir, rv.NIDBack))
	fmt.Printf("  Selfie    %s\n\n", filepath.Join(dir, rv.Selfie))
	fmt.Println("Check: the selfie matches the ID photo, the ID is real, readable and not expired,")
	fmt.Println("and the name on the ID matches the name above.")
	if rv.Status == verification.StatusPending {
		fmt.Printf("\nThen:  admin approve %s\n  or:  admin reject %s \"reason\"\n", rv.ID, rv.ID)
	}
	return nil
}

func decide(repo *verification.Repository, id string, approve bool, note string) error {
	rv, err := repo.GetForReview(id)
	if err != nil {
		return friendly(err)
	}
	if err := repo.Decide(id, approve, note, time.Now()); err != nil {
		return friendly(err)
	}
	if approve {
		fmt.Printf("Approved. %s is now a verified helper.\n", rv.UserName)
	} else {
		fmt.Printf("Rejected. %s will see: %q and can submit again.\n", rv.UserName, note)
	}
	return nil
}

func friendly(err error) error {
	switch {
	case errors.Is(err, verification.ErrNotFound):
		return errors.New("no submission with that id (copy it from `admin pending`)")
	case errors.Is(err, verification.ErrNotPending):
		return errors.New("that submission was already decided")
	}
	return err
}
