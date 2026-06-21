package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"careergps/internal/domain/coach"
	"careergps/pkg/apperrors"
)

type CoachSessionRepo struct {
	pool *pgxpool.Pool
}

func NewCoachSessionRepo(pool *pgxpool.Pool) *CoachSessionRepo {
	return &CoachSessionRepo{pool: pool}
}

func (r *CoachSessionRepo) Create(ctx context.Context, s *coach.CoachSession) error {
	// JD sessions use uuid.Nil as a sentinel for "no assessment" — store as SQL NULL
	var assessmentID interface{}
	if s.AssessmentID != uuid.Nil {
		assessmentID = s.AssessmentID
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO coach_sessions (id, candidate_id, assessment_id, context_snapshot, expires_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		s.ID, s.CandidateID, assessmentID, s.ContextSnapshot, s.ExpiresAt, s.CreatedAt,
	)
	return err
}

func (r *CoachSessionRepo) GetByID(ctx context.Context, id uuid.UUID) (*coach.CoachSession, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, candidate_id, assessment_id, context_snapshot, expires_at, created_at
		FROM coach_sessions WHERE id=$1`, id)
	var s coach.CoachSession
	var assessmentID *uuid.UUID
	var contextSnapshot []byte
	err := row.Scan(&s.ID, &s.CandidateID, &assessmentID, &contextSnapshot, &s.ExpiresAt, &s.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperrors.ErrNotFound
		}
		return nil, fmt.Errorf("coach_session: scan: %w", err)
	}
	if assessmentID != nil {
		s.AssessmentID = *assessmentID
	}
	s.ContextSnapshot = string(contextSnapshot)
	return &s, nil
}

func (r *CoachSessionRepo) ListByCandidateID(ctx context.Context, candidateID uuid.UUID, limit int, cursor string) ([]*coach.CoachSession, string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, candidate_id, assessment_id, context_snapshot, expires_at, created_at
		FROM coach_sessions WHERE candidate_id=$1 ORDER BY created_at DESC LIMIT $2`,
		candidateID, limit,
	)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var sessions []*coach.CoachSession
	for rows.Next() {
		var s coach.CoachSession
		var assessmentID *uuid.UUID
		var contextSnapshot []byte
		if err := rows.Scan(&s.ID, &s.CandidateID, &assessmentID, &contextSnapshot, &s.ExpiresAt, &s.CreatedAt); err != nil {
			return nil, "", err
		}
		if assessmentID != nil {
			s.AssessmentID = *assessmentID
		}
		s.ContextSnapshot = string(contextSnapshot)
		sessions = append(sessions, &s)
	}
	return sessions, "", nil
}

type CoachMessageRepo struct {
	pool *pgxpool.Pool
}

func NewCoachMessageRepo(pool *pgxpool.Pool) *CoachMessageRepo {
	return &CoachMessageRepo{pool: pool}
}

func (r *CoachMessageRepo) Create(ctx context.Context, m *coach.CoachMessage) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO coach_messages (id, session_id, role, content, token_cost, latency_ms, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		m.ID, m.SessionID, string(m.Role), m.Content, m.TokenCost, m.LatencyMs, m.CreatedAt,
	)
	return err
}

func (r *CoachMessageRepo) ListBySessionID(ctx context.Context, sessionID uuid.UUID) ([]*coach.CoachMessage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, session_id, role, content, token_cost, latency_ms, created_at
		FROM coach_messages WHERE session_id=$1 ORDER BY created_at ASC`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []*coach.CoachMessage
	for rows.Next() {
		var m coach.CoachMessage
		var role string
		if err := rows.Scan(&m.ID, &m.SessionID, &role, &m.Content, &m.TokenCost, &m.LatencyMs, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Role = coach.MessageRole(role)
		msgs = append(msgs, &m)
	}
	return msgs, nil
}

func (r *CoachMessageRepo) CountTodayByUserID(ctx context.Context, userID uuid.UUID) (int, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM coach_messages cm
		JOIN coach_sessions cs ON cm.session_id = cs.id
		WHERE cs.candidate_id IN (SELECT id FROM candidates WHERE user_id=$1)
		AND cm.role='user' AND cm.created_at >= $2`,
		userID, today,
	).Scan(&count)
	return count, err
}
