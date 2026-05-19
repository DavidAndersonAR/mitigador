// Package user manages bcrypt-hashed users for dashboard authentication.
//
// Filled in plan 01-04; see .planning/phases/01-observation-spine/01-04-PLAN.md.
package user

import (
	"errors"
	"time"
)

// BcryptCost is the bcrypt cost used for all password hashes.
// Per D-12 the cost MUST be at least 12.
const BcryptCost = 12

// User mirrors the public.users table.
type User struct {
	ID        int64
	Username  string
	Email     string
	CreatedAt time.Time
	LastLogin *time.Time
}

var (
	ErrNotFound      = errors.New("user: not found")
	ErrAlreadyExists = errors.New("user: already exists")
)
