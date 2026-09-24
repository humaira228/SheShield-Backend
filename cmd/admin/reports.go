// Reports/blocks moderation and the §6 fast-track helper suspension --
// split from main.go to keep that file to the original helper-verification
// commands. Same philosophy as verification: no network endpoint, only
// someone with server file access can run these.
package main

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zannatulmaliha/sheshield-backend/internal/alert"
	"github.com/zannatulmaliha/sheshield-backend/internal/audit"
	"github.com/zannatulmaliha/sheshield-backend/internal/auth"
	"github.com/zannatulmaliha/sheshield-backend/internal/helper"
	"github.com/zannatulmaliha/sheshield-backend/internal/matching"
	"github.com/zannatulmaliha/sheshield-backend/internal/ratelimit"
	"github.com/zannatulmaliha/sheshield-backend/internal/report"
)

const reportsUsage = `
  admin reports pending                       list reports waiting for review (both directions + system flags)
  admin reports show <id>                     one report's detail and the reported account's audit trail
  admin reports action <id> "resolution"       mark actioned
  admin reports action-false <id> "resolution" mark actioned AND flag the tied SOS as false (review-gated rate-limit signal)
  admin reports dismiss <id> "resolution"      mark dismissed, no action taken
  admin suspend-helper <uid> "reason"          fast-track: immediately suspends a helper and force-releases any SOS they hold
  admin fingerprint <uid> "reason"             read a device_fingerprint (multi-account abuse checks only) -- every read is audit-logged
`

func reportServiceFor(conn *sql.DB) *report.Service {
	return report.NewService(
		report.NewRepository(conn),
		audit.NewLogger(conn),
		ratelimit.NewRepository(conn),
		alert.NewRepository(conn), // satisfies report.SOSOwners via RequesterFor
	)
}

func reportsPending(svc *report.Service) error {
	queue, err := svc.Queue()
	if err != nil {
		return err
	}
	if len(queue) == 0 {
		fmt.Println("Nothing waiting for review.")
		return nil
	}
	fmt.Printf("%d waiting for review (oldest first):\n\n", len(queue))
	for _, rep := range queue {
		sos := ""
		if rep.SOSID != "" {
			sos = "  sos=" + rep.SOSID
		}
		fmt.Printf("  %s   %s reported %s   category=%s%s   filed %s\n",
			rep.ID, rep.ReporterID, rep.ReportedID, rep.Category, sos,
			rep.CreatedAt.Local().Format("2006-01-02 15:04"))
	}
	fmt.Println("\nLook at one with:  admin reports show <id>")
	return nil
}

func reportsShow(conn *sql.DB, svc *report.Service, id string) error {
	rep, err := svc.Get(id)
	if err != nil {
		return friendlyReportErr(err)
	}
	fmt.Printf("Report      %s\n", rep.ID)
	fmt.Printf("Status      %s\n", rep.ReviewStatus)
	fmt.Printf("Filed       %s\n", rep.CreatedAt.Local().Format("2006-01-02 15:04"))
	fmt.Printf("Reporter    %s (%s)\n", rep.ReporterID, rep.ReporterRole)
	fmt.Printf("Reported    %s\n", rep.ReportedID)
	fmt.Printf("Category    %s\n", rep.Category)
	if rep.SOSID != "" {
		fmt.Printf("SOS         %s\n", rep.SOSID)
	}
	if rep.ReviewedAt != nil {
		fmt.Printf("Reviewed    %s by %s -> %s (%s)\n",
			rep.ReviewedAt.Local().Format("2006-01-02 15:04"), rep.ReviewerID, rep.ReviewStatus, rep.Resolution)
	}

	trail, err := audit.NewLogger(conn).ForTarget(rep.ReportedID)
	if err == nil && len(trail) > 0 {
		fmt.Println("\nAudit trail for the reported account (oldest first):")
		for _, e := range trail {
			fmt.Printf("  %s  %-20s actor=%s\n", e.CreatedAt.Local().Format("2006-01-02 15:04"), e.Action, e.ActorID)
		}
	}

	if rep.ReviewStatus == report.StatusPending {
		fmt.Printf("\nThen:  admin reports action %s \"resolution\"\n  or:  admin reports dismiss %s \"resolution\"\n", rep.ID, rep.ID)
	}
	return nil
}

func reportsReview(svc *report.Service, id, status, resolution string, markFalse bool) error {
	err := svc.Review(id, "admin-cli", report.ReviewRequest{Status: status, Resolution: resolution, MarkFalseSOS: markFalse})
	if err != nil {
		return friendlyReportErr(err)
	}
	fmt.Printf("Report %s marked %s.\n", id, status)
	if markFalse {
		fmt.Println("The SOS's requester was recorded in sos_rate_limits' false_count (review-gated only -- no access was restricted).")
	}
	return nil
}

// suspendHelper is the spec's §6 fast-track exception: a credible report of
// a helper endangering a requester suspends them immediately (before any
// report review completes), and force-releases any SOS they currently hold
// so the standby queue can pick it back up right away.
func suspendHelper(conn *sql.DB, uid, reason string) error {
	authRepo := auth.NewRepository(conn)
	matchesRepo := matching.NewRepository(conn)
	helperRepo := helper.NewRepository(conn)
	auditLog := audit.NewLogger(conn)

	lockedSOS, err := matchesRepo.LockedSOSForHelper(uid)
	if err != nil {
		return err
	}
	if lockedSOS != "" {
		if err := helperRepo.Release(lockedSOS, uid, time.Now().UTC()); err != nil {
			return fmt.Errorf("force-releasing %s: %w", lockedSOS, err)
		}
		if _, err := matchesRepo.Release(lockedSOS, time.Now().UTC()); err != nil {
			return fmt.Errorf("releasing match row for %s: %w", lockedSOS, err)
		}
		fmt.Printf("Force-released SOS %s back to the standby queue.\n", lockedSOS)
	}

	if err := authRepo.SetHelperVerified(uid, false); err != nil {
		return err
	}
	_ = auditLog.Log("admin-cli", audit.ActionHelperSuspended, uid)

	fmt.Printf("Suspended helper %s. Reason: %q\n", uid, reason)
	fmt.Println("They will need to be re-verified (admin approve) before going active again.")
	return nil
}

// dispatchReports handles everything after `admin reports`, e.g. args ==
// ["pending"] or ["show", "<id>"].
func dispatchReports(conn *sql.DB, svc *report.Service, args []string) error {
	if len(args) == 0 {
		fmt.Print(reportsUsage)
		return nil
	}

	switch args[0] {
	case "pending":
		return reportsPending(svc)
	case "show":
		if len(args) != 2 {
			return errors.New("usage: admin reports show <id>")
		}
		return reportsShow(conn, svc, args[1])
	case "action":
		if len(args) != 3 {
			return errors.New(`usage: admin reports action <id> "resolution"`)
		}
		return reportsReview(svc, args[1], report.StatusActioned, args[2], false)
	case "action-false":
		if len(args) != 3 {
			return errors.New(`usage: admin reports action-false <id> "resolution"`)
		}
		return reportsReview(svc, args[1], report.StatusActioned, args[2], true)
	case "dismiss":
		if len(args) != 3 {
			return errors.New(`usage: admin reports dismiss <id> "resolution"`)
		}
		return reportsReview(svc, args[1], report.StatusDismissed, args[2], false)
	default:
		fmt.Print(reportsUsage)
		return nil
	}
}

// fingerprint reads a stored device_fingerprint -- multi-account abuse
// detection only (e.g. checking whether a banned user re-registered under a
// new account). Never shown to any party besides this abuse-review CLI, and
// every read is written to audit_log, same as any other sensitive-field
// access -- see the spec's data model notes on device_fingerprint.
func fingerprint(conn *sql.DB, uid, reason string) error {
	authRepo := auth.NewRepository(conn)
	fp, err := authRepo.DeviceFingerprint(uid)
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			return errors.New("no user with that uid")
		}
		return err
	}
	_ = audit.NewLogger(conn).Log("admin-cli", audit.ActionFingerprintRead, uid)

	if fp == "" {
		fmt.Printf("%s has no device_fingerprint on file (older account, or client didn't send one).\n", uid)
	} else {
		fmt.Printf("device_fingerprint for %s: %s\n", uid, fp)
	}
	fmt.Printf("(read logged to audit_log; reason given: %q)\n", reason)
	return nil
}

func friendlyReportErr(err error) error {
	switch {
	case errors.Is(err, report.ErrNotFound):
		return errors.New("no report with that id (copy it from `admin reports pending`)")
	case errors.Is(err, report.ErrAlreadyReviewed):
		return errors.New("that report was already reviewed")
	case errors.Is(err, report.ErrInvalidReview):
		return errors.New("status must be 'actioned' or 'dismissed'")
	}
	return err
}
