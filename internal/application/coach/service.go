package coachapp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"careergps/internal/domain/candidate"
	"careergps/internal/domain/coach"
	"careergps/internal/domain/gap"
	"careergps/internal/domain/readiness"
	"careergps/internal/domain/roadmap"
	"careergps/internal/infrastructure/llm"
	redisinfra "careergps/internal/infrastructure/redis"
	"careergps/pkg/apperrors"
)

const (
	coachMaxTokens    = 1500
	coachTemperature  = 0.7
	maxHistoryMessages = 20 // keep last 20 turns for context
)

type Service struct {
	sessionRepo    coach.SessionRepository
	messageRepo    coach.MessageRepository
	gapRepo        gap.Repository
	readinessRepo  readiness.Repository
	roadmapRepo    roadmap.Repository
	llm            llm.LLMProvider
	dailyCounter   *redisinfra.CoachDailyCounter
	dailyLimit     int
}

func NewService(
	sessionRepo coach.SessionRepository,
	messageRepo coach.MessageRepository,
	gapRepo gap.Repository,
	readinessRepo readiness.Repository,
	roadmapRepo roadmap.Repository,
	llmProvider llm.LLMProvider,
	counter *redisinfra.CoachDailyCounter,
	dailyLimit int,
) *Service {
	return &Service{
		sessionRepo:   sessionRepo,
		messageRepo:   messageRepo,
		gapRepo:       gapRepo,
		readinessRepo: readinessRepo,
		roadmapRepo:   roadmapRepo,
		llm:           llmProvider,
		dailyCounter:  counter,
		dailyLimit:    dailyLimit,
	}
}

// CreateSession starts a new coach session bound to an assessment.
func (s *Service) CreateSession(ctx context.Context, candidateID, assessmentID uuid.UUID) (*coach.CoachSession, error) {
	coachCtx, err := s.assembleContext(ctx, candidateID, assessmentID)
	if err != nil {
		return nil, fmt.Errorf("coach: assemble context: %w", err)
	}

	snapshot, err := json.Marshal(coachCtx)
	if err != nil {
		return nil, fmt.Errorf("coach: marshal context: %w", err)
	}

	sess, err := coach.NewSession(candidateID, assessmentID, string(snapshot))
	if err != nil {
		return nil, err
	}

	if err := s.sessionRepo.Create(ctx, sess); err != nil {
		return nil, fmt.Errorf("coach: create session: %w", err)
	}
	return sess, nil
}

// CreateJDSession starts a coach session anchored to a specific job.
// No assessmentID required — uses a nil UUID sentinel.
// The JD context and positioning data are injected directly into the system prompt.
func (s *Service) CreateJDSession(ctx context.Context, candidateID uuid.UUID, cand *candidate.Candidate, jdCtx *coach.JDContext) (*coach.CoachSession, error) {
	coachCtx := &coach.CoachContext{
		Candidate: cand,
		JDContext: jdCtx,
	}

	snapshot, err := json.Marshal(coachCtx)
	if err != nil {
		return nil, fmt.Errorf("coach: marshal context: %w", err)
	}

	// Use nil UUID for assessmentID — JD sessions are not bound to an assessment
	sess, err := coach.NewJDSession(candidateID, string(snapshot))
	if err != nil {
		return nil, err
	}

	if err := s.sessionRepo.Create(ctx, sess); err != nil {
		return nil, fmt.Errorf("coach: create jd session: %w", err)
	}
	return sess, nil
}

// SendMessageJD handles messages in a JD-aware session.
// Unlike SendMessage, it reconstructs context from the stored JDContext snapshot.
func (s *Service) SendMessageJD(ctx context.Context, userID uuid.UUID, sessionID uuid.UUID, content string) (<-chan llm.LLMChunk, error) {
	_, allowed, err := s.dailyCounter.Increment(ctx, userID.String())
	if err != nil {
		return nil, fmt.Errorf("coach: rate limit check: %w", err)
	}
	if !allowed {
		return nil, apperrors.ErrRateLimit
	}

	sess, err := s.sessionRepo.GetByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess.IsExpired() {
		return nil, apperrors.ErrNotFound
	}

	// Persist user message
	userMsg, err := coach.NewMessage(sessionID, coach.RoleUser, content)
	if err != nil {
		return nil, err
	}
	if err := s.messageRepo.Create(ctx, userMsg); err != nil {
		return nil, fmt.Errorf("coach: persist user message: %w", err)
	}

	// Reconstruct JD context from snapshot
	var coachCtx coach.CoachContext
	if err := json.Unmarshal([]byte(sess.ContextSnapshot), &coachCtx); err != nil {
		// Fallback: empty context
		coachCtx = coach.CoachContext{}
	}

	// Load conversation history
	history, err := s.messageRepo.ListBySessionID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("coach: load history: %w", err)
	}

	systemPrompt := coach.AssembleSystemPrompt(&coachCtx)
	userPrompt := buildUserPrompt(history, content)

	return s.llm.Stream(ctx, llm.LLMRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		MaxTokens:    coachMaxTokens,
		Temperature:  coachTemperature,
	})
}

// SendMessage persists the user message and returns a channel for the LLM stream.
// The caller (SSE handler) reads from the channel and forwards to the client.
func (s *Service) SendMessage(ctx context.Context, userID uuid.UUID, sessionID uuid.UUID, content string) (<-chan llm.LLMChunk, error) {
	// Rate limit check
	_, allowed, err := s.dailyCounter.Increment(ctx, userID.String())
	if err != nil {
		return nil, fmt.Errorf("coach: rate limit check: %w", err)
	}
	if !allowed {
		return nil, apperrors.ErrRateLimit
	}

	sess, err := s.sessionRepo.GetByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess.IsExpired() {
		return nil, apperrors.ErrNotFound
	}

	// Persist user message
	userMsg, err := coach.NewMessage(sessionID, coach.RoleUser, content)
	if err != nil {
		return nil, err
	}
	if err := s.messageRepo.Create(ctx, userMsg); err != nil {
		return nil, fmt.Errorf("coach: persist user message: %w", err)
	}

	// For JD sessions (AssessmentID == uuid.Nil), reconstruct context from the stored snapshot
	var coachCtx *coach.CoachContext
	if sess.AssessmentID == uuid.Nil {
		var snap coach.CoachContext
		if err := json.Unmarshal([]byte(sess.ContextSnapshot), &snap); err != nil {
			snap = coach.CoachContext{}
		}
		coachCtx = &snap
	} else {
		coachCtx, err = s.assembleContext(ctx, sess.CandidateID, sess.AssessmentID)
		if err != nil {
			return nil, fmt.Errorf("coach: assemble context: %w", err)
		}
	}

	// Load conversation history (last N turns)
	history, err := s.messageRepo.ListBySessionID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("coach: load history: %w", err)
	}

	systemPrompt := coach.AssembleSystemPrompt(coachCtx)
	userPrompt := buildUserPrompt(history, content)

	req := llm.LLMRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		MaxTokens:    coachMaxTokens,
		Temperature:  coachTemperature,
	}

	return s.llm.Stream(ctx, req)
}

// PersistAssistantMessage stores the completed assistant response after streaming.
func (s *Service) PersistAssistantMessage(ctx context.Context, sessionID uuid.UUID, content string, tokenCost int, latencyMs int64) error {
	msg, err := coach.NewMessage(sessionID, coach.RoleAssistant, content)
	if err != nil {
		return err
	}
	msg.TokenCost = tokenCost
	msg.LatencyMs = latencyMs
	return s.messageRepo.Create(ctx, msg)
}

func (s *Service) GetSession(ctx context.Context, id uuid.UUID) (*coach.CoachSession, error) {
	return s.sessionRepo.GetByID(ctx, id)
}

func (s *Service) assembleContext(ctx context.Context, candidateID, assessmentID uuid.UUID) (*coach.CoachContext, error) {
	ra, err := s.readinessRepo.GetByID(ctx, assessmentID)
	if err != nil {
		return nil, err
	}

	ga, err := s.gapRepo.GetByID(ctx, ra.GapAnalysisID)
	if err != nil {
		return nil, err
	}

	rm, _ := s.roadmapRepo.GetByAssessmentID(ctx, assessmentID) // roadmap may not exist yet

	var prepSummary *coach.PrepPlanSummary
	if rm != nil {
		prepSummary = coach.BuildPrepSummary(rm)
	}

	return &coach.CoachContext{
		GapAnalysis: ga,
		Assessment:  ra,
		PrepPlan:    prepSummary,
	}, nil
}

func buildUserPrompt(history []*coach.CoachMessage, currentMessage string) string {
	// Include last maxHistoryMessages turns
	start := 0
	if len(history) > maxHistoryMessages {
		start = len(history) - maxHistoryMessages
	}

	var sb []byte
	for _, m := range history[start:] {
		role := "User"
		if m.Role == coach.RoleAssistant {
			role = "Assistant"
		}
		sb = append(sb, []byte(fmt.Sprintf("%s: %s\n", role, m.Content))...)
	}
	sb = append(sb, []byte("User: "+currentMessage)...)
	return string(sb)
}

