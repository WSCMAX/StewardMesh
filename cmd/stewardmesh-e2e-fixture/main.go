// Command stewardmesh-e2e-fixture provisions the second local account used by
// the phase-one browser gate. It is intentionally unusable against a shared or
// non-disposable database.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/guard"
	postgresrepository "github.com/maxlemke/stewardmesh/internal/repository/postgres"
)

const (
	fixtureAcknowledgement = "yes-I-understand-this-is-disposable"
	readerAccountID        = "eeeeeeeeeeeeeeeeeeeeeeeeeeee0001"
	readerUsername         = "phase-one-reader"
	readerPassword         = "Phase-one-reader-password!"
	readerEmail            = "phase-one-reader@example.test"
	readerDisplayName      = "Phase One Reader"
	fixtureIssuer          = "https://e2e.invalid/stewardmesh"
	fixtureSubject         = "phase-one-reader"
	fixtureSource          = "oidc:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
)

var disposableDatabaseName = regexp.MustCompile(`^stewardmesh_e2e_[a-z0-9_]+$`)

func main() {
	var err error
	switch {
	case len(os.Args) == 1:
		err = run(context.Background(), os.Getenv)
	case len(os.Args) == 2 && os.Args[1] == "validate-target":
		err = validateEnvironment(os.Getenv)
	case len(os.Args) == 2 && os.Args[1] == "create-database":
		err = manageDatabase(context.Background(), os.Getenv, true)
	case len(os.Args) == 2 && os.Args[1] == "drop-database":
		err = manageDatabase(context.Background(), os.Getenv, false)
	default:
		err = errors.New("usage: stewardmesh-e2e-fixture [validate-target|create-database|drop-database]")
	}
	if err != nil {
		log.Fatal(err)
	}
	if len(os.Args) == 2 {
		if os.Args[1] == "create-database" {
			log.Print("disposable phase-one E2E database created")
			return
		}
		if os.Args[1] == "drop-database" {
			log.Print("disposable phase-one E2E database dropped")
			return
		}
		log.Print("disposable phase-one E2E target validated")
		return
	}
	log.Printf("phase-one reader fixture is ready: %s", readerUsername)
}

func manageDatabase(ctx context.Context, getenv func(string) string, create bool) error {
	if err := validateEnvironment(getenv); err != nil {
		return err
	}
	targetURL := strings.TrimSpace(getenv("STEWARDMESH_E2E_DATABASE_URL"))
	_, databaseName, err := parseLoopbackPostgresURL(targetURL)
	if err != nil {
		return err
	}
	adminURL := strings.TrimSpace(getenv("STEWARDMESH_E2E_POSTGRES_ADMIN_URL"))
	if adminURL == "" {
		return errors.New("STEWARDMESH_E2E_POSTGRES_ADMIN_URL is required for database lifecycle operations")
	}

	operation, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	database, err := postgresrepository.Open(operation, adminURL)
	if err != nil {
		return fmt.Errorf("open PostgreSQL maintenance database: %w", err)
	}
	defer database.Close()

	statement := fmt.Sprintf(`DROP DATABASE IF EXISTS "%s" WITH (FORCE)`, databaseName)
	if create {
		statement = fmt.Sprintf(`CREATE DATABASE "%s"`, databaseName)
	}
	if _, err := database.ExecContext(operation, statement); err != nil {
		return fmt.Errorf("manage disposable database: %w", err)
	}
	return nil
}

func run(ctx context.Context, getenv func(string) string) error {
	databaseURL := strings.TrimSpace(getenv("STEWARDMESH_E2E_DATABASE_URL"))
	organizationID := strings.TrimSpace(getenv("STEWARDMESH_E2E_ORGANIZATION_ID"))
	if err := validateEnvironment(getenv); err != nil {
		return err
	}

	operation, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	database, err := postgresrepository.Open(operation, databaseURL)
	if err != nil {
		return fmt.Errorf("open disposable PostgreSQL database: %w", err)
	}
	defer database.Close()

	var currentDatabase string
	if err := database.QueryRowContext(operation, "SELECT current_database()").Scan(&currentDatabase); err != nil {
		return fmt.Errorf("confirm disposable database: %w", err)
	}
	if !disposableDatabaseName.MatchString(currentDatabase) {
		return fmt.Errorf("refusing database %q: name must start with stewardmesh_e2e_", currentDatabase)
	}

	store, err := postgresrepository.NewGuardStore(database)
	if err != nil {
		return fmt.Errorf("initialize Guard fixture store: %w", err)
	}
	bootstrapRequired, err := store.BootstrapRequired(operation, organizationID)
	if err != nil {
		return fmt.Errorf("read administrator bootstrap state: %w", err)
	}
	if bootstrapRequired {
		return errors.New("refusing to provision the reader before the browser creates the disposable administrator")
	}

	now := time.Now().UTC()
	account, _, err := store.ProvisionExternalAccount(operation, guard.ExternalAccountProvisioning{
		Account: guard.Account{
			ID:                 readerAccountID,
			OrganizationID:     organizationID,
			Username:           readerUsername,
			NormalizedUsername: readerUsername,
			Email:              readerEmail,
			DisplayName:        readerDisplayName,
			Status:             "active",
			CreatedAt:          now,
			UpdatedAt:          now,
		},
		Identity: guard.ExternalIdentity{
			OrganizationID: organizationID,
			Issuer:         fixtureIssuer,
			Subject:        fixtureSubject,
			AccountID:      readerAccountID,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		AssignmentSource: fixtureSource,
	})
	if err != nil {
		return fmt.Errorf("provision disposable reader: %w", err)
	}
	passwordHash, err := guard.NewArgon2idHasher().Hash(readerPassword)
	if err != nil {
		return fmt.Errorf("hash disposable reader password: %w", err)
	}
	if err := store.UpdatePasswordHash(operation, account.ID, passwordHash, now); err != nil {
		return fmt.Errorf("enable disposable reader login: %w", err)
	}
	return nil
}

func validateEnvironment(getenv func(string) string) error {
	if getenv("STEWARDMESH_E2E_ALLOW_FIXTURES") != fixtureAcknowledgement {
		return errors.New("STEWARDMESH_E2E_ALLOW_FIXTURES must explicitly acknowledge a disposable database")
	}
	databaseURL := strings.TrimSpace(getenv("STEWARDMESH_E2E_DATABASE_URL"))
	if err := validateDisposableTarget(
		databaseURL,
		strings.TrimSpace(getenv("STEWARDMESH_E2E_ORGANIZATION_ID")),
	); err != nil {
		return err
	}
	adminURL := strings.TrimSpace(getenv("STEWARDMESH_E2E_POSTGRES_ADMIN_URL"))
	if adminURL != "" {
		adminParsed, databaseName, err := parseLoopbackPostgresURL(adminURL)
		if err != nil || adminParsed == nil || databaseName != "postgres" {
			return errors.New("STEWARDMESH_E2E_POSTGRES_ADMIN_URL must use loopback PostgreSQL and the postgres maintenance database")
		}
		targetParsed, _, err := parseLoopbackPostgresURL(databaseURL)
		if err != nil || targetParsed.Scheme != adminParsed.Scheme || targetParsed.Host != adminParsed.Host || databaseUser(targetParsed) != databaseUser(adminParsed) {
			return errors.New("E2E target and maintenance URLs must use the same PostgreSQL server and credentials")
		}
	}
	return nil
}

func databaseUser(parsed *url.URL) string {
	if parsed.User == nil {
		return ""
	}
	return parsed.User.String()
}

func validateDisposableTarget(databaseURL, organizationID string) error {
	if !strings.HasPrefix(organizationID, "e2e-") {
		return errors.New("STEWARDMESH_E2E_ORGANIZATION_ID must start with e2e-")
	}
	_, databaseName, err := parseLoopbackPostgresURL(databaseURL)
	if err != nil {
		return fmt.Errorf("STEWARDMESH_E2E_DATABASE_URL: %w", err)
	}
	if !disposableDatabaseName.MatchString(databaseName) {
		return errors.New("STEWARDMESH_E2E_DATABASE_URL database name must start with stewardmesh_e2e_")
	}
	return nil
}

func parseLoopbackPostgresURL(value string) (*url.URL, string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" || parsed.Hostname() == "" {
		return nil, "", errors.New("must be a PostgreSQL URL")
	}
	host := strings.ToLower(parsed.Hostname())
	address := net.ParseIP(host)
	if host != "localhost" && (address == nil || !address.IsLoopback()) {
		return nil, "", errors.New("must use a loopback PostgreSQL host")
	}
	databaseName := strings.TrimPrefix(parsed.EscapedPath(), "/")
	unescaped, unescapeErr := url.PathUnescape(databaseName)
	if unescapeErr != nil || unescaped == "" || strings.Contains(unescaped, "/") {
		return nil, "", errors.New("must contain one database name")
	}
	return parsed, unescaped, nil
}
