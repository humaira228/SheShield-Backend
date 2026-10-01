package helper

import (
	"errors"
	"testing"
	"time"
)

// seedLifecycle: one requester, one verified helper, one active alert.
func seedLifecycle(t *testing.T, trig string) (*Repository, string, string) {
	t.Helper()
	conn := openTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := conn.Exec(`
		INSERT INTO users (uid, name, email, password_hash, phone, country_code, gender, user_type, is_helper_verified, created_at)
		VALUES ('victim1', 'Nadia', 'n@example.com', 'x', '1712345678', '+880', 'female', 'user', 0, ?),
		       ('helper1', 'Hana', 'h@example.com', 'x', '1700000000', '+880', 'female', 'helper', 1, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`
		INSERT INTO alerts (id, user_uid, latitude, longitude, accuracy_m, created_at, updated_at, status, trigger_type)
		VALUES ('alert1', 'victim1', 23.8, 90.4, 10, ?, ?, 'active', ?)`, now, now, trig); err != nil {
		t.Fatal(err)
	}
	return NewRepository(conn), "alert1", "helper1"
}

func TestLifecycle_AcceptProgressResolveStatsHistory(t *testing.T) {
	repo, alertID, helperID := seedLifecycle(t, "motion_fall")
	t0 := time.Now().UTC().Add(-10 * time.Minute)

	if _, won, err := repo.Accept(alertID, helperID, t0); err != nil || !won {
		t.Fatalf("accept: won=%v err=%v", won, err)
	}

	cur, err := repo.Current(helperID)
	if err != nil || cur == nil {
		t.Fatalf("current: %v %v", cur, err)
	}
	if cur.Trigger != "motion_fall" || cur.RiskLevel != "high" || cur.Progress != "" {
		t.Errorf("unexpected current: %+v", cur)
	}

	// Cannot resolve before arriving.
	if err := repo.Resolve(alertID, helperID, t0.Add(time.Minute)); !errors.Is(err, ErrNeedArrival) {
		t.Fatalf("want ErrNeedArrival, got %v", err)
	}
	// Forward-only progress, idempotent repeat, no going back.
	if err := repo.SetProgress(alertID, helperID, ProgressEnRoute, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetProgress(alertID, helperID, ProgressEnRoute, t0.Add(time.Minute)); err != nil {
		t.Fatalf("repeat should be a no-op: %v", err)
	}
	if err := repo.SetProgress(alertID, helperID, ProgressArrived, t0.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetProgress(alertID, helperID, ProgressEnRoute, t0.Add(5*time.Minute)); !errors.Is(err, ErrBadProgress) {
		t.Fatalf("want ErrBadProgress, got %v", err)
	}
	// Someone else cannot touch it.
	if err := repo.SetProgress(alertID, "victim1", ProgressAssisting, t0); !errors.Is(err, ErrNotYourMatch) {
		t.Fatalf("want ErrNotYourMatch, got %v", err)
	}

	live, err := repo.Live(alertID, helperID)
	if err != nil || live.Status != "accepted" || live.Latitude == nil || live.Progress != ProgressArrived {
		t.Fatalf("live: %+v %v", live, err)
	}

	if err := repo.Resolve(alertID, helperID, t0.Add(8*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// Access is revoked: no coordinates, no current response.
	live, err = repo.Live(alertID, helperID)
	if err != nil || live.Status != "resolved" || live.Latitude != nil {
		t.Fatalf("after resolve live should hide position: %+v %v", live, err)
	}
	if cur, _ := repo.Current(helperID); cur != nil {
		t.Errorf("no current response expected after resolve")
	}

	st, err := repo.Stats(helperID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Responses != 1 || st.Resolved != 1 || st.SuccessRate != 100 {
		t.Errorf("stats: %+v", st)
	}
	if st.AvgResponseMinutes == nil || *st.AvgResponseMinutes != 4 {
		t.Errorf("avg response minutes: %v", st.AvgResponseMinutes)
	}

	hist, err := repo.History(helperID, 10)
	if err != nil || len(hist) != 1 || hist[0].Outcome != "resolved" || hist[0].Label != "Possible fall detected" {
		t.Fatalf("history: %+v %v", hist, err)
	}
}

func TestResolve_BlockedWhileDuressActive(t *testing.T) {
	repo, alertID, helperID := seedLifecycle(t, "manual")
	now := time.Now().UTC()
	if _, won, err := repo.Accept(alertID, helperID, now); err != nil || !won {
		t.Fatal(err)
	}
	_ = repo.SetProgress(alertID, helperID, ProgressAssisting, now)
	if _, err := repo.db.Exec(`INSERT INTO duress_signals (id, sos_id, type, triggered_at) VALUES ('d1', ?, 'manual_panic', ?)`,
		alertID, now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Resolve(alertID, helperID, now); !errors.Is(err, ErrDuressBlocksResolve) {
		t.Fatalf("want ErrDuressBlocksResolve, got %v", err)
	}
}

func TestRelease_RecordsReleasedAndClearsProgress(t *testing.T) {
	repo, alertID, helperID := seedLifecycle(t, "manual")
	now := time.Now().UTC()
	if _, _, err := repo.Accept(alertID, helperID, now); err != nil {
		t.Fatal(err)
	}
	_ = repo.SetProgress(alertID, helperID, ProgressEnRoute, now)
	if err := repo.Release(alertID, helperID, now); err != nil {
		t.Fatal(err)
	}
	st, _ := repo.Stats(helperID)
	if st.Responses != 1 || st.Resolved != 0 || st.Completed != 1 || st.SuccessRate != 0 {
		t.Errorf("stats after release: %+v", st)
	}
	var prog *string
	if err := repo.db.QueryRow(`SELECT helper_progress FROM alerts WHERE id = ?`, alertID).Scan(&prog); err != nil || prog != nil {
		t.Errorf("progress should be cleared: %v %v", prog, err)
	}
}

func TestActiveAlerts_CarriesTriggerAndDuress(t *testing.T) {
	repo, _, _ := seedLifecycle(t, "motion_sprint")
	alerts, err := repo.ActiveAlerts()
	if err != nil || len(alerts) != 1 {
		t.Fatalf("%v %v", alerts, err)
	}
	if alerts[0].Trigger != "motion_sprint" || alerts[0].DuressActive {
		t.Errorf("unexpected: %+v", alerts[0])
	}
}
