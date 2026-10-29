package planner

import "context"

// A disabled or recovered administrator must never reopen first-run setup.
const setupAvailableSQL = `SELECT NOT EXISTS(SELECT 1 FROM users WHERE admin)
 AND NOT EXISTS(SELECT 1 FROM settings WHERE key='bootstrap_consumed')`

func setupAvailable(ctx context.Context, q DBTX) (bool, error) {
	var available bool
	err := q.QueryRow(ctx, setupAvailableSQL).Scan(&available)
	return available, err
}
