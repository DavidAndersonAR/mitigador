package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Store provides CRUD against the users table.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore wraps a pgxpool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Create inserts a new user with a bcrypt-hashed password (cost = BcryptCost).
// Returns ErrAlreadyExists if the username is taken.
func (s *Store) Create(ctx context.Context, username, email, plaintextPassword string) (*User, error) {
	if username == "" || plaintextPassword == "" {
		return nil, fmt.Errorf("user: username and password are required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintextPassword), BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("user: bcrypt: %w", err)
	}
	var (
		id        int64
		createdAt time.Time
	)
	err = s.pool.QueryRow(ctx,
		`INSERT INTO users (username, email, password_hash) VALUES ($1, $2, $3) RETURNING id, created_at`,
		username, email, hash,
	).Scan(&id, &createdAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return nil, ErrAlreadyExists
		}
		return nil, fmt.Errorf("user: insert: %w", err)
	}
	return &User{ID: id, Username: username, Email: email, CreatedAt: createdAt}, nil
}

// Get fetches a user by username.
func (s *Store) Get(ctx context.Context, username string) (*User, error) {
	u, _, err := s.getWithHash(ctx, username)
	return u, err
}

// GetByID fetches a user by primary key (used by session middleware in plan 01-10).
func (s *Store) GetByID(ctx context.Context, id int64) (*User, error) {
	u := &User{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, COALESCE(email,''), created_at, last_login FROM users WHERE id=$1`, id,
	).Scan(&u.ID, &u.Username, &u.Email, &u.CreatedAt, &u.LastLogin)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user: get by id: %w", err)
	}
	return u, nil
}

func (s *Store) getWithHash(ctx context.Context, username string) (*User, []byte, error) {
	u := &User{}
	var hash []byte
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, COALESCE(email,''), password_hash, created_at, last_login FROM users WHERE username=$1`,
		username,
	).Scan(&u.ID, &u.Username, &u.Email, &hash, &u.CreatedAt, &u.LastLogin)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("user: get: %w", err)
	}
	return u, hash, nil
}

// VerifyPassword returns the user if the password matches; otherwise an error wrapping
// bcrypt.ErrMismatchedHashAndPassword (for the login handler to translate to 401).
func (s *Store) VerifyPassword(ctx context.Context, username, plaintextPassword string) (*User, error) {
	u, hash, err := s.getWithHash(ctx, username)
	if err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte(plaintextPassword)); err != nil {
		return nil, fmt.Errorf("user: verify: %w", err)
	}
	return u, nil
}

// List returns all users ordered by username.
func (s *Store) List(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, username, COALESCE(email,''), created_at, last_login FROM users ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("user: list: %w", err)
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.CreatedAt, &u.LastLogin); err != nil {
			return nil, fmt.Errorf("user: scan: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UpdatePassword re-hashes and stores a new password.
func (s *Store) UpdatePassword(ctx context.Context, username, newPlaintextPassword string) error {
	if newPlaintextPassword == "" {
		return fmt.Errorf("user: new password is required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPlaintextPassword), BcryptCost)
	if err != nil {
		return fmt.Errorf("user: bcrypt: %w", err)
	}
	tag, err := s.pool.Exec(ctx, `UPDATE users SET password_hash=$1 WHERE username=$2`, hash, username)
	if err != nil {
		return fmt.Errorf("user: update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a user.
func (s *Store) Delete(ctx context.Context, username string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE username=$1`, username)
	if err != nil {
		return fmt.Errorf("user: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateLastLogin sets last_login=now() for the given ID.
func (s *Store) UpdateLastLogin(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET last_login=now() WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("user: update last_login: %w", err)
	}
	return nil
}
