package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	authdomain "careergps/internal/domain/auth"
	"careergps/pkg/apperrors"
)

const bcryptCost = 12

type TokenPair struct {
	UserID       uuid.UUID
	AccessToken  string
	RefreshToken string
	ExpiresIn    int // seconds
}

type Claims struct {
	jwt.RegisteredClaims
}

type Service struct {
	userRepo        authdomain.UserRepository
	sessionRepo     authdomain.SessionRepository
	privateKey      interface{} // *rsa.PrivateKey
	publicKey       interface{} // *rsa.PublicKey
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
}

func NewService(
	userRepo authdomain.UserRepository,
	sessionRepo authdomain.SessionRepository,
	privateKey, publicKey interface{},
	accessTTLMin, refreshTTLDays int,
) *Service {
	return &Service{
		userRepo:        userRepo,
		sessionRepo:     sessionRepo,
		privateKey:      privateKey,
		publicKey:       publicKey,
		accessTokenTTL:  time.Duration(accessTTLMin) * time.Minute,
		refreshTokenTTL: time.Duration(refreshTTLDays) * 24 * time.Hour,
	}
}

func (s *Service) Register(ctx context.Context, email, password string) (*authdomain.User, *TokenPair, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: hash password: %w", err)
	}
	now := time.Now().UTC()
	user := &authdomain.User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: string(hash),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.userRepo.Create(ctx, user); err != nil {
		if errors.Is(err, apperrors.ErrConflict) {
			return nil, nil, apperrors.ErrConflict
		}
		return nil, nil, fmt.Errorf("auth: create user: %w", err)
	}
	tokens, err := s.issueTokens(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}
	return user, tokens, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (*TokenPair, error) {
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, apperrors.ErrUnauthorized
		}
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, apperrors.ErrUnauthorized
	}
	return s.issueTokens(ctx, user.ID)
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	sess, err := s.sessionRepo.GetByRefreshToken(ctx, refreshToken)
	if err != nil || sess.Revoked || time.Now().After(sess.ExpiresAt) {
		return nil, apperrors.ErrUnauthorized
	}
	if err := s.sessionRepo.Revoke(ctx, refreshToken); err != nil {
		return nil, err
	}
	return s.issueTokens(ctx, sess.UserID)
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	return s.sessionRepo.Revoke(ctx, refreshToken)
}

func (s *Service) issueTokens(ctx context.Context, userID uuid.UUID) (*TokenPair, error) {
	now := time.Now().UTC()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTokenTTL)),
			ID:        uuid.New().String(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	accessToken, err := token.SignedString(s.privateKey)
	if err != nil {
		return nil, fmt.Errorf("auth: sign token: %w", err)
	}

	refreshToken := generateRefreshToken()
	sess := &authdomain.Session{
		ID:           uuid.New(),
		UserID:       userID,
		RefreshToken: refreshToken,
		ExpiresAt:    now.Add(s.refreshTokenTTL),
		CreatedAt:    now,
	}
	if err := s.sessionRepo.Create(ctx, sess); err != nil {
		return nil, fmt.Errorf("auth: create session: %w", err)
	}

	return &TokenPair{
		UserID:       userID,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int(s.accessTokenTTL.Seconds()),
	}, nil
}

func generateRefreshToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
