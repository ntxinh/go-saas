package orgs

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	"github.com/jackc/pgx/v5/pgxpool"
)

// modelText is the RBAC-with-domains model: g(user, role, dom) grants the
// role's p-lines inside that domain; role templates use dom "*".
//
//go:embed model.conf
var modelText string

// pgAdapter persists casbin rules in the casbin_rule table. All queries
// run as the pool owner — policies are global (the org lives in v2), so
// no tenant tx and no RLS. persist.Adapter methods carry no ctx.
type pgAdapter struct{ pool *pgxpool.Pool }

func (a *pgAdapter) LoadPolicy(m model.Model) error {
	rows, err := a.pool.Query(context.Background(),
		`SELECT ptype, v0, v1, v2, v3, v4, v5 FROM casbin_rule`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ptype string
		var v [6]string
		if err := rows.Scan(&ptype, &v[0], &v[1], &v[2], &v[3], &v[4], &v[5]); err != nil {
			return err
		}
		// LoadPolicyLine keys the section off the first comma-separated
		// field, so join the non-empty tail — never insert empty v's.
		end := len(v)
		for end > 0 && v[end-1] == "" {
			end--
		}
		persist.LoadPolicyLine(ptype+","+strings.Join(v[:end], ","), m)
	}
	return rows.Err()
}

// SavePolicy is required by persist.Adapter but unused: all mutations go
// through AddPolicy/RemovePolicy/RemoveFilteredPolicy.
func (a *pgAdapter) SavePolicy(model.Model) error {
	return fmt.Errorf("casbin: SavePolicy unsupported")
}

func (a *pgAdapter) AddPolicy(_ string, ptype string, rule []string) error {
	v := pad(rule)
	_, err := a.pool.Exec(context.Background(),
		`INSERT INTO casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
		 VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`,
		ptype, v[0], v[1], v[2], v[3], v[4], v[5])
	return err
}

func (a *pgAdapter) RemovePolicy(_ string, ptype string, rule []string) error {
	v := pad(rule)
	_, err := a.pool.Exec(context.Background(),
		`DELETE FROM casbin_rule
		 WHERE ptype=$1 AND v0=$2 AND v1=$3 AND v2=$4 AND v3=$5 AND v4=$6 AND v5=$7`,
		ptype, v[0], v[1], v[2], v[3], v[4], v[5])
	return err
}

// RemoveFilteredPolicy deletes rules where each non-empty fieldValue
// matches, starting at column fieldIndex. "" wildcards, per casbin's
// adapter convention.
func (a *pgAdapter) RemoveFilteredPolicy(_ string, ptype string, fieldIndex int, fieldValues ...string) error {
	q := `DELETE FROM casbin_rule WHERE ptype=$1`
	args := []any{ptype}
	for i, val := range fieldValues {
		if val == "" {
			continue
		}
		args = append(args, val)
		q += fmt.Sprintf(` AND v%d=$%d`, fieldIndex+i, len(args))
	}
	_, err := a.pool.Exec(context.Background(), q, args...)
	return err
}

func pad(rule []string) [6]string {
	var v [6]string
	copy(v[:], rule)
	return v
}

// Enforcer is the app's authorization seam: a synced casbin enforcer over
// the pg adapter, plus the Grant/Revoke write side orgs.Service uses to
// keep g-lines in step with memberships.
type Enforcer struct {
	e *casbin.SyncedEnforcer
}

// NewEnforcer builds the enforcer: embedded model, pg adapter, load all
// policies, seed role templates idempotently.
func NewEnforcer(pool *pgxpool.Pool) (*Enforcer, error) {
	m, err := model.NewModelFromString(modelText)
	if err != nil {
		return nil, fmt.Errorf("casbin: model: %w", err)
	}
	e, err := casbin.NewSyncedEnforcer(m, &pgAdapter{pool: pool})
	if err != nil {
		return nil, fmt.Errorf("casbin: enforcer: %w", err)
	}
	// Seed role templates; AddPolicy is idempotent (ON CONFLICT +
	// in-memory dedup), so this is safe to run on every boot.
	for _, rule := range rolePolicies {
		if _, err := e.AddPolicy(rule); err != nil {
			return nil, fmt.Errorf("casbin: seed %v: %w", rule, err)
		}
	}
	return &Enforcer{e: e}, nil
}

// Enforce answers "may sub act on obj inside dom".
func (f *Enforcer) Enforce(sub, dom, obj, act string) (bool, error) {
	return f.e.Enforce("user:"+sub, "org:"+dom, obj, act)
}

// Grant records "user has role in org" as a g-line. Satisfies
// Service's Authorizer seam.
func (f *Enforcer) Grant(_ context.Context, orgID, userID, role string) error {
	_, err := f.e.AddGroupingPolicy("user:"+userID, "role:"+role, "org:"+orgID)
	return err
}

// Revoke drops every role the user holds in the org.
func (f *Enforcer) Revoke(_ context.Context, orgID, userID string) error {
	_, err := f.e.RemoveFilteredGroupingPolicy(0, "user:"+userID, "", "org:"+orgID)
	return err
}

// Invalidate reloads all policies; the RoleChanged/MemberRemoved
// subscriber calls it. In-process writes already update the enforcer, so
// this is a belt over the event contract.
//
// ponytail: full LoadPolicy per event — fine while casbin_rule is small;
// switch to LoadFilteredPolicy per org domain if the table grows.
func (f *Enforcer) Invalidate(_ string) { _ = f.e.LoadPolicy() }
