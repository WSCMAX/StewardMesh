package main

import "testing"

func TestValidateDisposableTarget(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name           string
		databaseURL    string
		organizationID string
		wantError      bool
	}{
		{name: "loopback", databaseURL: "postgres://user:password@127.0.0.1:5432/stewardmesh_e2e_phase_one?sslmode=disable", organizationID: "e2e-phase-one"},
		{name: "localhost", databaseURL: "postgresql://user:password@localhost/stewardmesh_e2e_ci", organizationID: "e2e-ci"},
		{name: "shared host", databaseURL: "postgres://db.example.test/stewardmesh_e2e_ci", organizationID: "e2e-ci", wantError: true},
		{name: "ordinary database", databaseURL: "postgres://localhost/stewardmesh", organizationID: "e2e-ci", wantError: true},
		{name: "ordinary organization", databaseURL: "postgres://localhost/stewardmesh_e2e_ci", organizationID: "production", wantError: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateDisposableTarget(test.databaseURL, test.organizationID)
			if test.wantError && err == nil {
				t.Fatal("expected validation failure")
			}
			if !test.wantError && err != nil {
				t.Fatalf("expected valid disposable target: %v", err)
			}
		})
	}
}

func TestValidateEnvironment(t *testing.T) {
	t.Parallel()
	base := map[string]string{
		"STEWARDMESH_E2E_ALLOW_FIXTURES":     fixtureAcknowledgement,
		"STEWARDMESH_E2E_DATABASE_URL":       "postgres://user:password@127.0.0.1:5432/stewardmesh_e2e_phase_one?sslmode=disable",
		"STEWARDMESH_E2E_POSTGRES_ADMIN_URL": "postgres://user:password@127.0.0.1:5432/postgres?sslmode=disable",
		"STEWARDMESH_E2E_ORGANIZATION_ID":    "e2e-phase-one",
	}
	for _, test := range []struct {
		name      string
		overrides map[string]string
		wantError bool
	}{
		{name: "valid"},
		{name: "missing acknowledgement", overrides: map[string]string{"STEWARDMESH_E2E_ALLOW_FIXTURES": ""}, wantError: true},
		{name: "remote maintenance host", overrides: map[string]string{"STEWARDMESH_E2E_POSTGRES_ADMIN_URL": "postgres://user:password@db.example.test:5432/postgres"}, wantError: true},
		{name: "wrong maintenance database", overrides: map[string]string{"STEWARDMESH_E2E_POSTGRES_ADMIN_URL": "postgres://user:password@127.0.0.1:5432/stewardmesh"}, wantError: true},
		{name: "different server", overrides: map[string]string{"STEWARDMESH_E2E_POSTGRES_ADMIN_URL": "postgres://user:password@127.0.0.1:6432/postgres"}, wantError: true},
		{name: "different credentials", overrides: map[string]string{"STEWARDMESH_E2E_POSTGRES_ADMIN_URL": "postgres://other:password@127.0.0.1:5432/postgres"}, wantError: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			environment := make(map[string]string, len(base))
			for name, value := range base {
				environment[name] = value
			}
			for name, value := range test.overrides {
				environment[name] = value
			}
			err := validateEnvironment(func(name string) string { return environment[name] })
			if test.wantError && err == nil {
				t.Fatal("expected validation failure")
			}
			if !test.wantError && err != nil {
				t.Fatalf("expected valid environment: %v", err)
			}
		})
	}
}
