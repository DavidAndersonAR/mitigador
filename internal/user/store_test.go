package user_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	pg "github.com/mitigador/mitigador/internal/storage/postgres"
	"github.com/mitigador/mitigador/internal/user"
)

func setup(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	dsn := os.Getenv("MITIGADOR_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("MITIGADOR_TEST_PG_DSN not set")
	}
	if err := pg.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pg.NewPool(ctx, dsn, 4, 1)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	// Clean users table before each test.
	_, _ = pool.Exec(context.Background(), "TRUNCATE users RESTART IDENTITY CASCADE")
	return pool, func() { pool.Close() }
}

func TestStore_CreateAndGet(t *testing.T) {
	pool, cleanup := setup(t)
	defer cleanup()
	s := user.NewStore(pool)
	u, err := s.Create(context.Background(), "ops", "ops@example.com", "CorrectHorseBatteryStaple")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.ID == 0 || u.Username != "ops" {
		t.Fatalf("unexpected user: %+v", u)
	}
	if u.Email != "ops@example.com" {
		t.Errorf("email mismatch: %q", u.Email)
	}
	got, err := s.Get(context.Background(), "ops")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != u.ID {
		t.Errorf("ID mismatch: %d vs %d", got.ID, u.ID)
	}
}

func TestStore_BcryptCostIs12(t *testing.T) {
	pool, cleanup := setup(t)
	defer cleanup()
	s := user.NewStore(pool)
	if _, err := s.Create(context.Background(), "ops", "", "anypassword123"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	var hash []byte
	if err := pool.QueryRow(context.Background(), `SELECT password_hash FROM users WHERE username='ops'`).Scan(&hash); err != nil {
		t.Fatalf("fetch hash: %v", err)
	}
	cost, err := bcrypt.Cost(hash)
	if err != nil {
		t.Fatalf("bcrypt.Cost: %v", err)
	}
	if cost != 12 {
		t.Errorf("bcrypt cost is %d, want 12", cost)
	}
	h := string(hash)
	if !strings.HasPrefix(h, "$2a$12$") && !strings.HasPrefix(h, "$2b$12$") && !strings.HasPrefix(h, "$2y$12$") {
		t.Fatalf("bcrypt cost is not 12: hash prefix = %q", h[:min(len(h), 7)])
	}
}

func TestStore_CreateDuplicate_ReturnsErrAlreadyExists(t *testing.T) {
	pool, cleanup := setup(t)
	defer cleanup()
	s := user.NewStore(pool)
	_, err := s.Create(context.Background(), "ops", "", "somepassword123")
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err = s.Create(context.Background(), "ops", "", "somepassword123")
	if !errors.Is(err, user.ErrAlreadyExists) {
		t.Errorf("expected ErrAlreadyExists, got: %v", err)
	}
}

func TestStore_GetMissing_ReturnsErrNotFound(t *testing.T) {
	pool, cleanup := setup(t)
	defer cleanup()
	s := user.NewStore(pool)
	_, err := s.Get(context.Background(), "nobody")
	if !errors.Is(err, user.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestStore_VerifyPassword_Success(t *testing.T) {
	pool, cleanup := setup(t)
	defer cleanup()
	s := user.NewStore(pool)
	_, err := s.Create(context.Background(), "ops", "", "CorrectPass12345")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	u, err := s.VerifyPassword(context.Background(), "ops", "CorrectPass12345")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if u.ID == 0 {
		t.Errorf("expected non-zero ID, got 0")
	}
}

func TestStore_VerifyPassword_WrongPassword(t *testing.T) {
	pool, cleanup := setup(t)
	defer cleanup()
	s := user.NewStore(pool)
	_, err := s.Create(context.Background(), "ops", "", "CorrectPass12345")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = s.VerifyPassword(context.Background(), "ops", "WrongPass99999")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		t.Errorf("expected bcrypt.ErrMismatchedHashAndPassword in chain, got: %v", err)
	}
}

func TestStore_UpdatePassword(t *testing.T) {
	pool, cleanup := setup(t)
	defer cleanup()
	s := user.NewStore(pool)
	_, err := s.Create(context.Background(), "ops", "", "OldPass12345678")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.UpdatePassword(context.Background(), "ops", "NewPass12345678"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
	// Old password must fail.
	_, err = s.VerifyPassword(context.Background(), "ops", "OldPass12345678")
	if err == nil {
		t.Error("old password should no longer work")
	}
	// New password must succeed.
	_, err = s.VerifyPassword(context.Background(), "ops", "NewPass12345678")
	if err != nil {
		t.Errorf("new password should work, got: %v", err)
	}
}

func TestStore_Delete(t *testing.T) {
	pool, cleanup := setup(t)
	defer cleanup()
	s := user.NewStore(pool)
	_, err := s.Create(context.Background(), "ops", "", "somepassword123")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Delete(context.Background(), "ops"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = s.Get(context.Background(), "ops")
	if !errors.Is(err, user.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got: %v", err)
	}
	// Deleting a non-existent user returns ErrNotFound.
	if err := s.Delete(context.Background(), "ghost"); !errors.Is(err, user.ErrNotFound) {
		t.Errorf("expected ErrNotFound for ghost, got: %v", err)
	}
}

func TestStore_List(t *testing.T) {
	pool, cleanup := setup(t)
	defer cleanup()
	s := user.NewStore(pool)
	names := []string{"alice", "bob", "carol"}
	for _, name := range names {
		if _, err := s.Create(context.Background(), name, name+"@example.com", "somepassword123"); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	users, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("expected 3 users, got %d", len(users))
	}
	// Should be ordered by username ASC.
	if users[0].Username != "alice" || users[1].Username != "bob" || users[2].Username != "carol" {
		t.Errorf("wrong order: %v", users)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
