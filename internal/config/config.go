package config

import (
	"os"
)

// Config holds everything the app reads from the environment. Nothing here
// has a hardcoded secret default — JWTSecret must be set or the app refuses
// to start (see cmd/api/main.go).
type Config struct {
	Port        string
	DBPath      string
	JWTSecret   string
	JWTTTLHours int
	CORSOrigin  string

	// SMSProvider picks how the server sends texts. "log" (default) only
	// prints them, so nothing is sent or billed. See internal/sms.
	SMSProvider string

	// PushProvider picks how the server alarms a linked trusted contact's
	// phone. "log" (default) only prints it -- no Firebase project needed
	// until you set this to "fcm" and provide the two fields below. See
	// internal/push.
	PushProvider       string
	FCMCredentialsPath string
	FCMProjectID       string

	// UploadDir is where helper verification photos are stored. These are
	// identity documents: keep it outside anything that is served publicly.
	UploadDir string

	// PublicBaseURL prefixes the no-login /track/<token> link sent in SOS
	// texts, e.g. "https://api.sheshield.example". Must be reachable by a
	// contact's phone browser, so the local-dev default only works when
	// testing against a device on the same machine/network as the server.
	PublicBaseURL string

	// AdminAPIKey gates the /api/v1/admin/* moderation routes (see
	// internal/adminapi + middleware.RequireAdminKey). There is no "admin"
	// user role/JWT in this app on purpose -- only someone holding this
	// separate, operator-issued key can reach the queue at all. Empty
	// (the default) means the admin HTTP surface is disabled entirely;
	// cmd/api/main.go only registers those routes when this is set, so a
	// deployment that never sets it behaves exactly as before (CLI-only,
	// same as cmd/admin always was).
	AdminAPIKey string
}

func Load() Config {
	loadDotEnv(".env")
	return Config{
		Port:               getEnv("PORT", "8080"),
		DBPath:             getEnv("DB_PATH", "./data/sheshield.db"),
		JWTSecret:          os.Getenv("JWT_SECRET"),
		JWTTTLHours:        24 * 7, // 7 days, matches a typical "stay signed in" mobile app
		CORSOrigin:         getEnv("CORS_ORIGIN", "*"),
		SMSProvider:        getEnv("SMS_PROVIDER", "log"),
		PushProvider:       getEnv("PUSH_PROVIDER", "log"),
		FCMCredentialsPath: getEnv("FCM_CREDENTIALS_PATH", ""),
		FCMProjectID:       getEnv("FCM_PROJECT_ID", ""),
		UploadDir:          getEnv("UPLOAD_DIR", "./data/uploads"),
		PublicBaseURL:      getEnv("PUBLIC_BASE_URL", "http://localhost:8080"),
		AdminAPIKey:        os.Getenv("ADMIN_API_KEY"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}