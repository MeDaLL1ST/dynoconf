package store

import "context"

// RegisterConnection records a newly opened gRPC stream.
func (s *Store) RegisterConnection(ctx context.Context, serviceID int64, replicaID, connID, peerAddr string) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO connection_clients (service_id, replica_id, conn_id, peer_addr)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (replica_id, conn_id)
		 DO UPDATE SET updated_at = now()`,
		serviceID, replicaID, connID, peerAddr)
	return err
}

// UnregisterConnection removes a closed/broken stream.
func (s *Store) UnregisterConnection(ctx context.Context, replicaID, connID string) error {
	_, err := s.Pool.Exec(ctx,
		`DELETE FROM connection_clients WHERE replica_id = $1 AND conn_id = $2`, replicaID, connID)
	return err
}

// TouchReplicaConnections refreshes updated_at for all of a replica's rows
// (heartbeat), so live connections aren't reaped by the TTL.
func (s *Store) TouchReplicaConnections(ctx context.Context, replicaID string) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE connection_clients SET updated_at = now() WHERE replica_id = $1`, replicaID)
	return err
}

// ActiveConnections counts live streams for a service (fresh within ttlSeconds).
func (s *Store) ActiveConnections(ctx context.Context, serviceID int64, ttlSeconds int) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM connection_clients
		 WHERE service_id = $1 AND updated_at > now() - make_interval(secs => $2)`,
		serviceID, ttlSeconds).Scan(&n)
	return n, err
}

// ActiveConnectionsAll returns the live count per service id.
func (s *Store) ActiveConnectionsAll(ctx context.Context, ttlSeconds int) (map[int64]int, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT service_id, COUNT(*) FROM connection_clients
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

// ListConnectionClients returns live streams for a service (for the detail view).
func (s *Store) ListConnectionClients(ctx context.Context, serviceID, ttlSeconds int64) ([]ConnectionClient, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT service_id, replica_id, conn_id, peer_addr, connected_at
		 FROM connection_clients
		 WHERE service_id = $1 AND updated_at > now() - make_interval(secs => $2)
		 ORDER BY replica_id, connected_at`, serviceID, ttlSeconds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ConnectionClient{}
	for rows.Next() {
		var c ConnectionClient
		if err := rows.Scan(&c.ServiceID, &c.ReplicaID, &c.ConnID, &c.PeerAddr, &c.ConnectedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PurgeStaleConnections removes connection rows older than ttlSeconds.
func (s *Store) PurgeStaleConnections(ctx context.Context, ttlSeconds int) error {
	_, err := s.Pool.Exec(ctx,
		`DELETE FROM connection_clients WHERE updated_at < now() - make_interval(secs => $1)`,
		ttlSeconds)
	return err
}

// ClearReplicaConnections removes all rows for a replica (graceful shutdown).
func (s *Store) ClearReplicaConnections(ctx context.Context, replicaID string) error {
	_, err := s.Pool.Exec(ctx,
		`DELETE FROM connection_clients WHERE replica_id = $1`, replicaID)
	return err
}
