package main

import (
	"log"
	"net/http"

	"github.com/zannatulmaliha/sheshield-backend/internal/alert"
	"github.com/zannatulmaliha/sheshield-backend/internal/auth"
	"github.com/zannatulmaliha/sheshield-backend/internal/config"
	"github.com/zannatulmaliha/sheshield-backend/internal/contact"
	"github.com/zannatulmaliha/sheshield-backend/internal/db"
	"github.com/zannatulmaliha/sheshield-backend/internal/helper"
	"github.com/zannatulmaliha/sheshield-backend/internal/push"
	"github.com/zannatulmaliha/sheshield-backend/internal/sms"
	"github.com/zannatulmaliha/sheshield-backend/internal/verification"
)

func main() {
	cfg := config.Load()
	if cfg.JWTSecret == "" {
		log.Fatal("JWT_SECRET is not set. Set it in your environment or .env before starting the server.")
	}

	if cfg.JWTSecret == "change-me-to-a-long-random-value" {
		log.Println("WARNING: JWT_SECRET is still the example placeholder. Fine for local dev, but anyone can forge logins with it -- set a real random value before deploying.")
	}

	conn, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer conn.Close()

	mux := http.NewServeMux()

	authRepo := auth.NewRepository(conn)
	authService := auth.NewService(authRepo, cfg.JWTSecret, cfg.JWTTTLHours)
	auth.NewHandler(authService).Register(mux, cfg.JWTSecret)

	contactRepo := contact.NewRepository(conn)
	contact.NewHandler(contactRepo).Register(mux, cfg.JWTSecret)

	sender, err := sms.New(cfg.SMSProvider)
	if err != nil {
		log.Fatalf("sms: %v", err)
	}
	if !sender.Live() {
		log.Println("SMS: log-only mode -- the server does NOT send real texts (alerts are recorded, messages are only printed here). Set SMS_PROVIDER once a provider is configured.")
	}

	pusher, err := push.New(cfg.PushProvider, cfg.FCMCredentialsPath, cfg.FCMProjectID)
	if err != nil {
		log.Fatalf("push: %v", err)
	}
	if !pusher.Live() {
		log.Println("Push: log-only mode -- linked trusted contacts do NOT get an alarm push (only printed here). Set PUSH_PROVIDER=fcm with FCM_CREDENTIALS_PATH/FCM_PROJECT_ID once a Firebase project is configured.")
	}

	alertService := alert.NewService(authRepo, contactRepo, alert.NewRepository(conn), sender, pusher, cfg.PublicBaseURL)
	alertHandler := alert.NewHandler(alertService)
	alertHandler.Register(mux, cfg.JWTSecret)
	alertHandler.RegisterPublic(mux) // no-login tracking page + its JSON feed -- deliberately outside RequireAuth

	verificationService := verification.NewService(authRepo, verification.NewRepository(conn), cfg.UploadDir)
	verification.NewHandler(verificationService).Register(mux, cfg.JWTSecret)

	helperRepo := helper.NewRepository(conn)
	helperService := helper.NewService(authRepo, helperRepo, helperRepo)
	helper.NewHandler(helperService).Register(mux, cfg.JWTSecret)

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	handler := withCORS(cfg.CORSOrigin, mux)

	log.Printf("sheshield-backend listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, handler); err != nil {
		log.Fatal(err)
	}
}

// withCORS lets the Flutter app (web/dev builds especially) call this API
// from a different origin. Lock CORS_ORIGIN down to your real app origin(s)
// in production instead of "*".
func withCORS(origin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
