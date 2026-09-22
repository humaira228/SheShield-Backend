package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// fcmScope is the only OAuth scope this server needs from the service
// account -- just enough to call the Send API, nothing else in the Firebase
// project.
const fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

// fcmSender calls FCM's HTTP v1 API directly instead of pulling in the full
// firebase-admin-go SDK (which drags in most of google-cloud-go). All that's
// actually needed is an OAuth2-authenticated HTTP client and one POST.
type fcmSender struct {
	client    *http.Client
	projectID string
}

// NewFCMSender reads a Firebase service-account JSON key (Firebase Console
// -> Project settings -> Service accounts -> Generate new private key) from
// credentialsPath and authenticates as it.
func NewFCMSender(credentialsPath, projectID string) (Sender, error) {
	if credentialsPath == "" || projectID == "" {
		return nil, fmt.Errorf("push: FCM_CREDENTIALS_PATH and FCM_PROJECT_ID must both be set for PUSH_PROVIDER=fcm")
	}
	raw, err := os.ReadFile(credentialsPath)
	if err != nil {
		return nil, fmt.Errorf("push: reading FCM_CREDENTIALS_PATH: %w", err)
	}
	ctx := context.Background()
	creds, err := google.CredentialsFromJSON(ctx, raw, fcmScope)
	if err != nil {
		return nil, fmt.Errorf("push: parsing FCM service account: %w", err)
	}
	return &fcmSender{
		client:    oauth2.NewClient(ctx, creds.TokenSource),
		projectID: projectID,
	}, nil
}

func (s *fcmSender) Live() bool { return true }

// Send delivers a data-only message (no "notification" field): that's what
// keeps Android delivering it to the app's own background handler in every
// app state (foreground/background/killed) instead of letting the system
// tray swallow it while the app is backgrounded, which a notification+data
// payload would do.
func (s *fcmSender) Send(ctx context.Context, token, senderName string, lat, lng float64) error {
	payload := map[string]any{
		"message": map[string]any{
			"token": token,
			"data": map[string]string{
				"type":       "sos_alarm",
				"senderName": senderName,
				"latitude":   fmt.Sprintf("%f", lat),
				"longitude":  fmt.Sprintf("%f", lng),
			},
			"android": map[string]any{
				"priority": "high",
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", s.projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("push: fcm %s: %s", resp.Status, respBody)
	}
	return nil
}
