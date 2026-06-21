package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authdomain "careergps/internal/domain/auth"
	"careergps/pkg/apperrors"
)

type PasswordResetRepo struct {
	pool *pgxpool.Pool
}

func NewPasswordResetRepo(pool *pgxpool.Pool) *PasswordResetRepo {
	return &PasswordResetRepo{pool: pool}
}

func (r *PasswordResetRepo) Create(ctx context.Context, t *authdomain.PasswordResetToken) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO password_reset_tokens (id, user_id, token, expires_at, used, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		t.ID, t.UserID, t.Token, t.ExpiresAt, t.Used, t.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("password_reset: create: %w", err)
	}
	return nil
}

func (r *PasswordResetRepo) GetByToken(ctx context.Context, token string) (*authdomain.PasswordResetToken, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, user_id, token, expires_at, used, created_at
		FROM password_reset_tokens WHERE token=$1`, token)
	var t authdomain.PasswordResetToken
	err := row.Scan(&t.ID, &t.UserID, &t.Token, &t.ExpiresAt, &t.Used, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("password_reset: get: %w", err)
	}
	return &t, nil
}

func (r *PasswordResetRepo) MarkUsed(ctx context.Context, token string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE password_reset_tokens SET used=TRUE WHERE token=$1`, token)
	return err
}
