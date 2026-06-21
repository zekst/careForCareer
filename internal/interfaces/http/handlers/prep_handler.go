package handlers

import (
	"context"
	"errors"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	coachapp "careergps/internal/application/coach"
	"careergps/internal/domain/coach"
	"careergps/internal/infrastructure/llm"
	"careergps/internal/infrastructure/postgres"
	"careergps/internal/infrastructure/sse"
	"careergps/internal/interfaces/http/middleware"
	"careergps/pkg/apperrors"
)

// PrepHandler handles JD-aware coach sessions and prep plan generation.
type PrepHandler struct {
	coachSvc      *coachapp.Service
	candidateRepo *postgres.CandidateRepo
	llmProvider   llm.LLMProvider
}

func NewPrepHandler(coachSvc *coachapp.Service, candidateRepo *postgres.CandidateRepo, llmProvider llm.LLMProvider) *PrepHandler {
	return &PrepHandler{
		coachSvc:      coachSvc,
		candidateRepo: candidateRepo,
		llmProvider:   llmProvider,
	}
}

// ── JD-Aware Coach Session ────────────────────────────────────────────────────

type createJDSessionRequest struct {
	JobTitle       string   `json:"job_title" binding:"required,max=200"`
	Company        string   `json:"company" binding:"omitempty,max=200"`
	Location       string   `json:"location" binding:"omitempty,max=200"`
	JDText         string   `json:"jd_text" binding:"omitempty,max=20000"`
	OverallMatch   int      `json:"overall_match"`
	TierFit        string   `json:"tier_fit"`
	CompanyBar     string   `json:"company_bar"`
	Summary        string   `json:"summary"`
	SkillGaps      []string `json:"skill_gaps"`
	ActionPlan     []string `json:"action_plan"`
	InterviewFocus []string `json:"interview_focus"`
	TimeToReady    string   `json:"time_to_ready"`
}

// CreateJDSession godoc
// POST /api/v1/coach/jd-sessions
func (h *PrepHandler) CreateJDSession(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorEnvelope("UNAUTHORIZED", "Not authenticated"))
		return
	}

	var req createJDSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorEnvelope("VALIDATION_ERROR", err.Error()))
		return
	}

	// Resolve candidate.id from user.id — coach_sessions FK references candidates(id)
	cand, err := h.candidateRepo.GetByUserID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, errorEnvelope("NOT_FOUND", "Create your profile first"))
		return
	}

	jdCtx := &coach.JDContext{
		JobTitle:       req.JobTitle,
		Company:        req.Company,
		Location:       req.Location,
		JDText:         req.JDText,
		OverallMatch:   req.OverallMatch,
		TierFit:        req.TierFit,
		CompanyBar:     req.CompanyBar,
		Summary:        req.Summary,
		SkillGaps:      req.SkillGaps,
		ActionPlan:     req.ActionPlan,
		InterviewFocus: req.InterviewFocus,
		TimeToReady:    req.TimeToReady,
	}

	sess, err := h.coachSvc.CreateJDSession(c.Request.Context(), cand.ID, cand, jdCtx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorEnvelope("INTERNAL_ERROR", "Could not create JD coach session"))
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"session_id": sess.ID,
		"expires_at": sess.ExpiresAt,
		"mode":       "jd_aware",
	})
}

// StreamJD is the SSE endpoint for JD-aware coach sessions.
// GET /api/v1/coach/jd-sessions/:id/stream?message=...&token=...
func (h *PrepHandler) StreamJD(c *gin.Context) {
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

	content := c.Query("message")
	if content == "" {
		c.JSON(http.StatusBadRequest, errorEnvelope("MISSING_MESSAGE", "message query param required"))
		return
	}
	if len(content) > 2000 {
		c.JSON(http.StatusBadRequest, errorEnvelope("MESSAGE_TOO_LONG", "Message must be under 2000 characters"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()

	sseWriter, err := sse.New(c.Writer)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorEnvelope("SSE_ERROR", "Streaming not supported"))
		return
	}

	stream, err := h.coachSvc.SendMessageJD(ctx, userID, sessionID, content)
	if err != nil {
		if errors.Is(err, apperrors.ErrRateLimit) {
			_ = sseWriter.WriteError("RATE_LIMIT", "Daily coach message limit reached")
			return
		}
		_ = sseWriter.WriteError("LLM_UNAVAILABLE", "Coach is temporarily unavailable")
		return
	}

	var fullContent strings.Builder
	var tokenCount int
	start := time.Now()

	for chunk := range stream {
		if chunk.Error != nil {
			_ = sseWriter.WriteError("LLM_STREAM_ERROR", "Response interrupted")
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

	go func() {
		bgCtx := context.Background()
		_ = h.coachSvc.PersistAssistantMessage(bgCtx, sessionID, fullContent.String(), tokenCount, latencyMs)
	}()
}

// ── Prep Plan Generator ───────────────────────────────────────────────────────

// PrepWeek is one week in the structured study plan.
type PrepWeek struct {
	Week      int       `json:"week"`
	Title     string    `json:"title"`
	Focus     string    `json:"focus"`
	Days      []PrepDay `json:"days"`
	Milestone string    `json:"milestone"`
}

// PrepDay is a single day's study block.
type PrepDay struct {
	Day      int      `json:"day"`
	Label    string   `json:"label"` // e.g. "Monday"
	Topics   []string `json:"topics"`
	Task     string   `json:"task"`
	Resource string   `json:"resource,omitempty"`
	Duration string   `json:"duration"` // e.g. "2-3 hours"
}

// PrepPlanResponse is returned to the frontend.
type PrepPlanResponse struct {
	JobTitle       string     `json:"job_title"`
	Company        string     `json:"company"`
	TotalWeeks     int        `json:"total_weeks"`
	TimeToReady    string     `json:"time_to_ready"`
	OverallMatch   int        `json:"overall_match"`
	Weeks          []PrepWeek `json:"weeks"`
	FinalTip       string     `json:"final_tip"`
	GeneratedAt    string     `json:"generated_at"`
}

type generatePrepPlanRequest struct {
	JobTitle       string   `json:"job_title" binding:"required"`
	Company        string   `json:"company"`
	OverallMatch   int      `json:"overall_match"`
	TierFit        string   `json:"tier_fit"`
	CompanyBar     string   `json:"company_bar"`
	TimeToReady    string   `json:"time_to_ready"`
	SkillGaps      []string `json:"skill_gaps"`
	ActionPlan     []string `json:"action_plan"`
	InterviewFocus []string `json:"interview_focus"`
	YOE            int      `json:"yoe"`
}

// GeneratePrepPlan godoc
// POST /api/v1/jobs/prep-plan
func (h *PrepHandler) GeneratePrepPlan(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorEnvelope("UNAUTHORIZED", "Not authenticated"))
		return
	}
	_ = userID

	var req generatePrepPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorEnvelope("VALIDATION_ERROR", err.Error()))
		return
	}

	// Determine week count from time_to_ready
	weeks := estimateWeeks(req.TimeToReady)

	systemPrompt := `You are a senior engineering interview coach. Generate a structured, week-by-week study plan for a software engineer preparing for a specific role.

Return ONLY valid JSON matching this exact schema — no markdown, no extra text:
{
  "total_weeks": <integer>,
  "weeks": [
    {
      "week": <integer>,
      "title": "<week theme, e.g. 'DSA Fundamentals'>",
      "focus": "<one sentence on what this week builds>",
      "days": [
        {
          "day": <1-5>,
          "label": "<Mon|Tue|Wed|Thu|Fri>",
          "topics": ["<topic1>", "<topic2>"],
          "task": "<specific task, e.g. 'Solve 10 LeetCode medium DP problems'>",
          "resource": "<optional: specific book/course/list>",
          "duration": "<e.g. '2 hours'>"
        }
      ],
      "milestone": "<what the candidate should be able to do by end of week>"
    }
  ],
  "final_tip": "<one honest, specific tip for the final days before interview>"
}

Rules:
- Day 6 and 7 are rest/review — do NOT include them in the days array (Mon-Fri only, day 1-5)
- Tasks must be specific: "Solve Blind 75 arrays section" not "practice arrays"
- Resources must be real: "Grokking System Design (educative.io)", "CLRS Ch. 4", "NeetCode 150"
- If company bar is "high" (FAANG), add a system design week for candidates with 3+ YOE
- Final week should always be mock interviews + review, not new topics
- Match depth to candidate's YOE and match score — don't overload a junior`

	userPrompt := fmt.Sprintf(`CANDIDATE:
- YOE: %d
- Role: %s at %s
- Match Score: %d%% | Tier Fit: %s | Company Bar: %s
- Time to Ready: %s
- Skill Gaps: %s
- Priority Actions: %s
- Expected Interview Topics: %s
- Plan for %d weeks`,
		req.YOE,
		req.JobTitle,
		orDefault(req.Company, "the company"),
		req.OverallMatch,
		orDefault(req.TierFit, "match"),
		orDefault(req.CompanyBar, "medium"),
		orDefault(req.TimeToReady, fmt.Sprintf("%d weeks", weeks)),
		strings.Join(req.SkillGaps, ", "),
		strings.Join(req.ActionPlan, "; "),
		strings.Join(req.InterviewFocus, ", "),
		weeks,
	)

	resp, err := h.llmProvider.Generate(c.Request.Context(), llm.LLMRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		MaxTokens:    4500,
		Temperature:  0.3,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorEnvelope("LLM_ERROR", "Prep plan generation failed"))
		return
	}

	// Parse LLM response
	type llmPlan struct {
		TotalWeeks int        `json:"total_weeks"`
		Weeks      []PrepWeek `json:"weeks"`
		FinalTip   string     `json:"final_tip"`
	}

	raw := extractJSON(resp.Content)

	var plan llmPlan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		c.JSON(http.StatusInternalServerError, errorEnvelope("LLM_PARSE_ERROR", "Prep plan generation returned unexpected format — please try again"))
		return
	}

	if len(plan.Weeks) == 0 {
		c.JSON(http.StatusInternalServerError, errorEnvelope("EMPTY_PLAN", "Prep plan returned no weeks"))
		return
	}

	c.JSON(http.StatusOK, PrepPlanResponse{
		JobTitle:     req.JobTitle,
		Company:      req.Company,
		TotalWeeks:   plan.TotalWeeks,
		TimeToReady:  req.TimeToReady,
		OverallMatch: req.OverallMatch,
		Weeks:        plan.Weeks,
		FinalTip:     plan.FinalTip,
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
	})
}

// estimateWeeks parses a "time_to_ready" string like "3-4 weeks" into an integer.
func estimateWeeks(s string) int {
	s = strings.ToLower(s)
	// Extract first number
	var n int
	for _, field := range strings.Fields(s) {
		if _, err := fmt.Sscan(field, &n); err == nil && n > 0 {
			return n
		}
		// Handle "3-4" range — take the higher number for safety
		if strings.Contains(field, "-") {
			parts := strings.Split(field, "-")
			for _, p := range parts {
				var x int
				if _, err := fmt.Sscan(p, &x); err == nil && x > n {
					n = x
				}
			}
			if n > 0 {
				return n
			}
		}
	}
	if n == 0 {
		return 4 // sensible default
	}
	return n
}
