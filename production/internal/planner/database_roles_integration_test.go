package planner

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestDatabaseRolesIntegration(t *testing.T) {
	dsn := os.Getenv("GMA_TEST_DSN")
	if dsn == "" {
		t.Skip("explicit isolated PostgreSQL required")
	}
	base, e := url.Parse(dsn)
	if e != nil || base.Path != "/gma_planner_test" {
		t.Fatal("refusing non-test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	admin, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal("test database unavailable")
	}
	defer admin.DB.Close()
	suffix := newID()
	schemaName := "roles_" + suffix
	foreign := "foreign_" + suffix
	migrator := "migration_" + suffix
	runtime := "runtime_" + suffix
	password := token()
	for _, name := range []string{migrator, runtime} {
		if _, e = admin.DB.Exec(ctx, "CREATE ROLE "+pgx.Identifier{name}.Sanitize()+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD $1", pgx.QueryExecModeSimpleProtocol, password); e != nil {
			t.Fatal("cannot create synthetic role")
		}
		defer admin.DB.Exec(context.Background(), "DROP ROLE "+pgx.Identifier{name}.Sanitize())
	}
	if _, e = admin.DB.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schemaName}.Sanitize()+" AUTHORIZATION "+pgx.Identifier{migrator}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	defer admin.DB.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schemaName}.Sanitize()+" CASCADE")
	if _, e = admin.DB.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{foreign}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	defer admin.DB.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{foreign}.Sanitize()+" CASCADE")
	if _, e = admin.DB.Exec(ctx, "CREATE TABLE "+pgx.Identifier{foreign, "secret"}.Sanitize()+"(value text)"); e != nil {
		t.Fatal(e)
	}
	connect := func(role string) *Store {
		u := *base
		u.User = url.UserPassword(role, password)
		q := u.Query()
		q.Set("search_path", schemaName)
		u.RawQuery = q.Encode()
		s, err := Open(ctx, u.String())
		if err != nil {
			t.Fatal("synthetic role connection failed")
		}
		return s
	}
	migration := connect(migrator)
	defer migration.DB.Close()
	if e = migration.MigrateForRuntime(ctx, runtime); e != nil {
		t.Fatal("non-superuser migration", e)
	}
	appStore := connect(runtime)
	defer appStore.DB.Close()
	if e = appStore.CheckRuntimePrivileges(ctx); e != nil {
		t.Fatal(e)
	}
	if e = migration.CheckRuntimePrivileges(ctx); e == nil {
		t.Fatal("migration owner accepted as runtime")
	}
	if e = admin.CheckRuntimePrivileges(ctx); e == nil {
		t.Fatal("superuser accepted as runtime")
	}
	// Reject dangerous privileges even outside the configured search_path.
	runtimeID := pgx.Identifier{runtime}.Sanitize()
	foreignID := pgx.Identifier{foreign}.Sanitize()
	for _, change := range [][2]string{
		{"GRANT CREATE ON DATABASE gma_planner_test TO " + runtimeID, "REVOKE CREATE ON DATABASE gma_planner_test FROM " + runtimeID},
		{"GRANT CREATE ON SCHEMA " + foreignID + " TO " + runtimeID, "REVOKE CREATE ON SCHEMA " + foreignID + " FROM " + runtimeID},
		{"ALTER TABLE " + pgx.Identifier{foreign, "secret"}.Sanitize() + " OWNER TO " + runtimeID, "ALTER TABLE " + pgx.Identifier{foreign, "secret"}.Sanitize() + " OWNER TO CURRENT_USER"},
	} {
		if _, e = admin.DB.Exec(ctx, change[0]); e != nil {
			t.Fatal(e)
		}
		guardErr := appStore.CheckRuntimePrivileges(ctx)
		if _, e = admin.DB.Exec(ctx, change[1]); e != nil {
			t.Fatal(e)
		}
		if guardErr == nil {
			t.Fatal("runtime accepted database CREATE, foreign schema CREATE or foreign object ownership")
		}
		if e = appStore.CheckRuntimePrivileges(ctx); e != nil {
			t.Fatal(e)
		}
	}
	app, e := NewApp(appStore, Config{Origin: "https://localhost:9443", WebDir: t.TempDir(), Secret: random(32), Bootstrap: token()})
	if e != nil {
		t.Fatal(e)
	}
	project, calendar, _ := fixture()
	project.People = nil
	project.Tasks[0].Assignments = nil
	if e = app.SeedCalendar(ctx, calendar); e != nil {
		t.Fatal(e)
	}
	if e = app.Import(ctx, project, "", "roles-test"); e != nil {
		t.Fatal("runtime import", e)
	}
	if _, e = readProject(ctx, appStore.DB, project.ID); e != nil {
		t.Fatal("runtime read", e)
	}
	if _, e = appStore.DB.Exec(ctx, "UPDATE projects SET title='Synthetic updated' WHERE id=$1", project.ID); e != nil {
		t.Fatal("runtime update", e)
	}
	if e = app.workerTick(ctx); e != nil {
		t.Fatal("runtime worker", e)
	}
	if _, e = appStore.DB.Exec(ctx, "DELETE FROM projects WHERE id=$1", project.ID); e != nil {
		t.Fatal("runtime delete", e)
	}
	forbidden := []string{"CREATE ROLE " + pgx.Identifier{"forbidden_" + suffix}.Sanitize(), "CREATE DATABASE " + pgx.Identifier{"forbidden_" + suffix}.Sanitize(), "CREATE TABLE forbidden(value text)", "UPDATE schema_versions SET checksum='forbidden'", "SELECT * FROM " + pgx.Identifier{foreign, "secret"}.Sanitize(), "SET ROLE " + pgx.Identifier{migrator}.Sanitize()}
	for _, sql := range forbidden {
		_, err := appStore.DB.Exec(ctx, sql)
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "42501" {
			t.Fatalf("expected insufficient_privilege, got %T (%v)", err, err)
		}
	}
	// An additive migration must re-grant access without giving DDL/ownership.
	if _, e = migration.DB.Exec(ctx, "CREATE TABLE future_application_table(id text)"); e != nil {
		t.Fatal(e)
	}
	if e = migration.MigrateForRuntime(ctx, runtime); e != nil {
		t.Fatal(e)
	}
	if _, e = appStore.DB.Exec(ctx, "INSERT INTO future_application_table VALUES('synthetic')"); e != nil {
		t.Fatal(e)
	}
	if e = appStore.CheckRuntimePrivileges(ctx); e != nil {
		t.Fatal(e)
	}
	t.Log("PASS: non-superuser migration; runtime import/read/update/delete/worker; denied CREATE ROLE/DB/DDL, migration metadata, foreign schema and SET ROLE; future table grants")
}
