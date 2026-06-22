package httpserver

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// connTTL is the freshness window (seconds) for aggregating active connection
// counts from service_connections. Kept in sync with grpcserver.ConnTTLSeconds.
const connTTL = 30

// isUniqueViolation reports whether err is a Postgres unique-constraint error.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
