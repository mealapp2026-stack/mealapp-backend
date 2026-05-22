package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	AppEnv       string
	HTTPAddr     string
	MongoURI     string
	MongoDB      string
	JWTSecret    string
	TokenTTL     time.Duration
	OpenAIAPIKey string
	OpenAIModel  string
}

func Load() Config {
	ttlHours := intFromEnv("TOKEN_TTL_HOURS", 168)

	return Config{
		AppEnv:       stringFromEnv("APP_ENV", "development"),
		HTTPAddr:     httpAddrFromEnv(),
		MongoURI:     stringFromEnv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:      stringFromEnv("MONGO_DATABASE", "mealapp"),
		JWTSecret:    stringFromEnv("JWT_SECRET", "dev-secret-change-me"),
		TokenTTL:     time.Duration(ttlHours) * time.Hour,
		OpenAIAPIKey: stringFromEnv("OPENAI_API_KEY", ""),
		OpenAIModel:  stringFromEnv("OPENAI_MODEL", "gpt-5.2"),
	}
}

func httpAddrFromEnv() string {
	if value := os.Getenv("HTTP_ADDR"); value != "" {
		return value
	}
	if port := os.Getenv("PORT"); port != "" {
		return ":" + port
	}
	return ":8085"
}

func stringFromEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func intFromEnv(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
