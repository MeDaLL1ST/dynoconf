package store

import "context"

// UpsertConnectionCount records this replica's current active stream count for
// a service. Called on a heartbeat by each replica.
func (s *Store) UpsertConnectionCount(ctx context.Context, serviceID int64, replicaID string, active int) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO service_connections (service_id, replica_id, active_count, updated_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (service_id, replica_id)
		 DO UPDATE SET active_count = EXCLUDED.active_count, updated_at = now()`,
		serviceID, replicaID, active)
	return err
}

// ActiveConnections returns the aggregated count of live gRPC streams for a
// service across all replicas whose heartbeat is fresher than ttlSeconds.
func (s *Store) ActiveConnections(ctx context.Context, serviceID int64, ttlSeconds int) (int, error) {
	var total int
	err := s.Pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(active_count), 0) FROM service_connections
		 WHERE service_id = $1 AND updated_at > now() - make_interval(secs => $2)`,
		serviceID, ttlSeconds).Scan(&total)
	return total, err
}

// ActiveConnectionsAll returns the aggregated live count per service id.
func (s *Store) ActiveConnectionsAll(ctx context.Context, ttlSeconds int) (map[int64]int, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT service_id, COALESCE(SUM(active_count), 0) FROM service_connections
		 WHERE updated_at > now() - make_interval(secs => $1)
		 GROUP BY service_id`, ttlSeconds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// PurgeStaleConnections removes connection rows older than ttlSeconds. Also used
// at shutdown by a replica to clear its own rows (via ClearReplicaConnections).
func (s *Store) PurgeStaleConnections(ctx context.Context, ttlSeconds int) error {
	_, err := s.Pool.Exec(ctx,
		`DELETE FROM service_connections WHERE updated_at < now() - make_interval(secs => $1)`,
		ttlSeconds)
	return err
}

// ClearReplicaConnections removes all rows for a given replica (graceful
// shutdown).
func (s *Store) ClearReplicaConnections(ctx context.Context, replicaID string) error {
	_, err := s.Pool.Exec(ctx,
		`DELETE FROM service_connections WHERE replica_id = $1`, replicaID)
	return err
}
