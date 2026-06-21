package config

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port        string
	DatabaseURL string
	JWTSecret   string
	BaseURL     string
	WA          WAConfig
	SMTP        SMTPConfig
}

type WAConfig struct {
	PhoneNumberID      string
	WABAID             string
	AccessToken        string
	WebhookVerifyToken string
	AppSecret          string
}

type SMTPConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	From     string
}

// Load reads configuration from environment variables, optionally loading a .env file first.
func Load() (*Config, error) {
	_ = godotenv.Load() // silently ignore missing .env in production

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET is not set; refusing to start. Generate one with: openssl rand -hex 32")
	}

	cfg := &Config{
		Port:        env("PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		JWTSecret:   jwtSecret,
		BaseURL:     env("BASE_URL", "http://localhost:8080"),
		WA: WAConfig{
			PhoneNumberID:      os.Getenv("WA_PHONE_NUMBER_ID"),
			WABAID:             os.Getenv("WA_WABA_ID"),
			AccessToken:        os.Getenv("WA_ACCESS_TOKEN"),
			WebhookVerifyToken: os.Getenv("WA_WEBHOOK_VERIFY_TOKEN"),
			AppSecret:          os.Getenv("WA_APP_SECRET"),
		},
		SMTP: SMTPConfig{
			Host:     os.Getenv("SMTP_HOST"),
			Port:     env("SMTP_PORT", "587"),
			User:     os.Getenv("SMTP_USER"),
			Password: os.Getenv("SMTP_PASSWORD"),
			From:     env("SMTP_FROM", "noreply@theoffbeat.agency"),
		},
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	return cfg, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
