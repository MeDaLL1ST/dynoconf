package store

import "context"

// AddFavorite stars a service for a user.
func (s *Store) AddFavorite(ctx context.Context, userID, serviceID int64) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO user_favorites (user_id, service_id) VALUES ($1, $2)
		 ON CONFLICT DO NOTHING`, userID, serviceID)
	return err
}

// RemoveFavorite unstars a service.
func (s *Store) RemoveFavorite(ctx context.Context, userID, serviceID int64) error {
	_, err := s.Pool.Exec(ctx,
		`DELETE FROM user_favorites WHERE user_id = $1 AND service_id = $2`, userID, serviceID)
	return err
}

// ListFavorites returns the service ids a user has starred.
func (s *Store) ListFavorites(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT service_id FROM user_favorites WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
