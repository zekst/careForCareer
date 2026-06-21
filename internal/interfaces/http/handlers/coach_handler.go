package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	coachapp "careergps/internal/application/coach"
	"careergps/internal/infrastructure/postgres"
	"careergps/internal/infrastructure/sse"
	"careergps/internal/interfaces/http/middleware"
	"careergps/pkg/apperrors"
)

type CoachHandler struct {
	coachSvc      *coachapp.Service
	candidateRepo *postgres.CandidateRepo
}

func NewCoachHandler(coachSvc *coachapp.Service, candidateRepo *postgres.CandidateRepo) *CoachHandler {
	return &CoachHandler{coachSvc: coachSvc, candidateRepo: candidateRepo}
}

type createSessionRequest struct {
	AssessmentID string `json:"assessment_id" binding:"required,uuid"`
}

type sendMessageRequest struct {
	Content string `json:"content" binding:"required,min=1,max=2000"`
}

func (h *CoachHandler) CreateSession(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorEnvelope("UNAUTHORIZED", "Not authenticated"))
		return
	}

	var req createSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorEnvelope("VALIDATION_ERROR", err.Error()))
		return
	}

	assessmentID, err := uuid.Parse(req.AssessmentID)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorEnvelope("INVALID_UUID", "Invalid assessment_id"))
		return
	}

	// Resolve candidate.id from user.id — coach_sessions FK references candidates(id)
	cand, err := h.candidateRepo.GetByUserID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, errorEnvelope("NOT_FOUND", "Create your profile first"))
		return
	}
	sess, err := h.coachSvc.CreateSession(c.Request.Context(), cand.ID, assessmentID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			c.JSON(http.StatusNotFound, errorEnvelope("NOT_FOUND", "Assessment not found"))
			return
		}
		c.JSON(http.StatusInternalServerError, errorEnvelope("INTERNAL_ERROR", "Could not create coach session"))
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"session_id": sess.ID,
		"expires_at": sess.ExpiresAt,
	})
}

func (h *CoachHandler) GetSession(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errorEnvelope("INVALID_UUID", "Invalid session ID"))
		return
	}
	sess, err := h.coachSvc.GetSession(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, errorEnvelope("NOT_FOUND", "Session not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"session_id":   sess.ID,
		"assessment_id": sess.AssessmentID,
		"created_at":   sess.CreatedAt,
		"expires_at":   sess.ExpiresAt,
	})
}

func (h *CoachHandler) SendMessage(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorEnvelope("UNAUTHORIZED", "Not authenticated"))
		return
	}

	sessionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errorEnvelope("INVALID_UUID", "Invalid session ID"))
		return
	}

	var req sendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorEnvelope("VALIDATION_ERROR", err.Error()))
		return
	}

	_, err = h.coachSvc.SendMessage(c.Request.Context(), userID, sessionID, req.Content)
	if err != nil {
		if errors.Is(err, apperrors.ErrRateLimit) {
			c.JSON(http.StatusTooManyRequests, errorEnvelope("RATE_LIMIT", "Daily coach message limit reached (20/day)"))
			return
		}
		if errors.Is(err, apperrors.ErrNotFound) {
			c.JSON(http.StatusNotFound, errorEnvelope("NOT_FOUND", "Session not found or expired"))
			return
		}
		c.JSON(http.StatusInternalServerError, errorEnvelope("INTERNAL_ERROR", "Failed to send message"))
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"status": "processing"})
}

// Stream handles the SSE endpoint for coach responses.
// Client connects here after SendMessage — server streams the LLM response token-by-token.
func (h *CoachHandler) Stream(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorEnvelope("UNAUTHORIZED", "Not authenticated"))
		return
	}

	sessionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errorEnvelope("INVALID_UUID", "Invalid session ID"))
		return
	}

	// Get the last user message content for this session to stream a response
	content := c.Query("message")
	if content == "" {
		c.JSON(http.StatusBadRequest, errorEnvelope("MISSING_MESSAGE", "message query param required"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()

	sseWriter, err := sse.New(c.Writer)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorEnvelope("SSE_ERROR", "Streaming not supported"))
		return
	}

	stream, err := h.coachSvc.SendMessage(ctx, userID, sessionID, content)
	if err != nil {
		if errors.Is(err, apperrors.ErrRateLimit) {
			_ = sseWriter.WriteError("RATE_LIMIT", "Daily coach message limit reached")
			return
		}
		_ = sseWriter.WriteError("LLM_UNAVAILABLE", "Coach is temporarily unavailable. Your plan and gap analysis remain accessible.")
		return
	}

	var fullContent strings.Builder
	var tokenCount int
	start := time.Now()

	for chunk := range stream {
		if chunk.Error != nil {
			// Mid-stream error: close with error event. Never surface partial response as complete.
			_ = sseWriter.WriteError("LLM_STREAM_ERROR", "Response interrupted. No partial answer was sent.")
			return
		}
		if chunk.Done {
			break
		}
		fullContent.WriteString(chunk.Delta)
		_ = sseWriter.WriteDelta(chunk.Delta)
	}

	latencyMs := time.Since(start).Milliseconds()
	_ = sseWriter.WriteDone(sessionID.String(), tokenCount, latencyMs)

	// Persist assistant message asynchronously — don't block SSE close
	go func() {
		bgCtx := context.Background()
		_ = h.coachSvc.PersistAssistantMessage(bgCtx, sessionID, fullContent.String(), tokenCount, latencyMs)
	}()
}
