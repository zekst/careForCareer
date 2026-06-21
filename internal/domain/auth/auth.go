package authdomain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// User is the core identity entity.
type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Session represents an active refresh-token session.
type Session struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	RefreshToken string
	ExpiresAt    time.Time
	Revoked      bool
	CreatedAt    time.Time
}

// UserRepository is the persistence interface for users.
type UserRepository interface {
	Create(ctx context.Context, u *User) error
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	UpdatePassword(ctx context.Context, userID uuid.UUID, passwordHash string) error
}

// SessionRepository is the persistence interface for refresh-token sessions.
type SessionRepository interface {
	Create(ctx context.Context, s *Session) error
	GetByRefreshToken(ctx context.Context, token string) (*Session, error)
	Revoke(ctx context.Context, token string) error
}

// PasswordResetToken represents a one-time password-reset token.
type PasswordResetToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Token     string
	ExpiresAt time.Time
	Used      bool
	CreatedAt time.Time
}

// PasswordResetRepository is the persistence interface for password-reset tokens.
type PasswordResetRepository interface {
	Create(ctx context.Context, t *PasswordResetToken) error
	GetByToken(ctx context.Context, token string) (*PasswordResetToken, error)
	MarkUsed(ctx context.Context, token string) error
}
