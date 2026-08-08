package config

import "os"

type Config struct {
	Addr           string
	DataDir        string
	BlobDir        string
	DatabaseURL    string
	AllowedOrigin  string
	OrganizationID string
}

func FromEnv() Config {
	return Config{
		Addr:           envOr("STEWARDMESH_ADDR", ":8080"),
		DataDir:        envOr("STEWARDMESH_DATA_DIR", "./data"),
		BlobDir:        envOr("STEWARDMESH_BLOB_DIR", "./storage"),
		DatabaseURL:    envOr("STEWARDMESH_DATABASE_URL", "postgres://stewardmesh:stewardmesh@localhost:5432/stewardmesh?sslmode=disable"),
		AllowedOrigin:  envOr("STEWARDMESH_ALLOWED_ORIGIN", "http://localhost:5173"),
		OrganizationID: envOr("STEWARDMESH_ORGANIZATION_ID", "local-organization"),
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
