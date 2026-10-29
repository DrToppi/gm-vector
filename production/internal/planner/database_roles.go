package planner

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"os"
)

type MigrationConfig struct {
	DatabaseURL string `json:"database_url"`
	RuntimeRole string `json:"runtime_role"`
}

func LoadMigrationConfig(path string) (MigrationConfig, error) {
	var c MigrationConfig
	b, e := os.ReadFile(path)
	if e != nil {
		return c, errors.New("migration configuration unavailable")
	}
	if json.Unmarshal(b, &c) != nil || c.DatabaseURL == "" || c.RuntimeRole == "" {
		return c, errors.New("migration configuration invalid")
	}
	return c, nil
}

// Fail closed at runtime entrypoints. Membership itself is prohibited because
// even a NOINHERIT membership can permit SET ROLE to a stronger principal.
func (s *Store) CheckRuntimePrivileges(ctx context.Context) error {
	var unsafe bool
	e := s.DB.QueryRow(ctx, `SELECT r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls
 OR EXISTS(SELECT 1 FROM pg_auth_members m WHERE m.member=r.oid)
 OR has_database_privilege(current_user,current_database(),'CREATE')
 OR EXISTS(SELECT 1 FROM pg_namespace n WHERE n.nspname NOT LIKE 'pg\_%' ESCAPE '\' AND n.nspname<>'information_schema' AND (n.nspowner=r.oid OR has_schema_privilege(current_user,n.oid,'CREATE')))
 OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT LIKE 'pg\_%' ESCAPE '\' AND n.nspname<>'information_schema' AND c.relowner=r.oid)
 OR EXISTS(SELECT 1 FROM pg_database d WHERE d.datname=current_database() AND d.datdba=r.oid)
 FROM pg_roles r WHERE r.rolname=current_user`).Scan(&unsafe)
	if e != nil {
		return errors.New("cannot verify runtime database privileges")
	}
	if unsafe {
		return errors.New("runtime database role is privileged; run database role setup before starting API or worker")
	}
	return nil
}

func grantRuntime(ctx context.Context, tx pgx.Tx, role string) error {
	var unsafe bool
	e := tx.QueryRow(ctx, `SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls OR EXISTS(SELECT 1 FROM pg_auth_members m WHERE m.member=r.oid) FROM pg_roles r WHERE rolname=$1`, role).Scan(&unsafe)
	if e != nil || unsafe {
		return errors.New("invalid runtime database role")
	}
	var schemaName string
	if e = tx.QueryRow(ctx, "SELECT current_schema()").Scan(&schemaName); e != nil {
		return e
	}
	ident := pgx.Identifier{role}.Sanitize()
	schemaID := pgx.Identifier{schemaName}.Sanitize()
	if _, e = tx.Exec(ctx, "REVOKE CREATE ON SCHEMA "+schemaID+" FROM PUBLIC, "+ident); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "GRANT USAGE ON SCHEMA "+schemaID+" TO "+ident); e != nil {
		return e
	}
	// Only relations owned by the migration principal are in this grant set.
	rows, e := tx.Query(ctx, `SELECT c.relname,c.relkind::text FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_roles r ON r.oid=c.relowner WHERE n.nspname=current_schema() AND r.rolname=current_user AND c.relkind IN ('r','p','S')`)
	if e != nil {
		return e
	}
	type rel struct{ name, kind string }
	var relations []rel
	for rows.Next() {
		var v rel
		if e = rows.Scan(&v.name, &v.kind); e != nil {
			rows.Close()
			return e
		}
		relations = append(relations, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, v := range relations {
		target := pgx.Identifier{schemaName, v.name}.Sanitize()
		if v.kind == "S" {
			_, e = tx.Exec(ctx, "GRANT USAGE,SELECT ON SEQUENCE "+target+" TO "+ident)
		} else {
			if _, e = tx.Exec(ctx, "REVOKE ALL ON TABLE "+target+" FROM "+ident); e != nil {
				return e
			}
			grants := "SELECT,INSERT,UPDATE,DELETE"
			if v.name == "schema_versions" {
				grants = "SELECT"
			}
			_, e = tx.Exec(ctx, "GRANT "+grants+" ON TABLE "+target+" TO "+ident)
		}
		if e != nil {
			return e
		}
	}
	return nil
}
