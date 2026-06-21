package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"careergps/internal/infrastructure/llm"
	"careergps/internal/infrastructure/postgres"
	"careergps/internal/interfaces/http/middleware"
)

// PositioningHandler analyses a candidate's fit for a specific job.
type PositioningHandler struct {
	candidateRepo *postgres.CandidateRepo
	llm           llm.LLMProvider
}

func NewPositioningHandler(candidateRepo *postgres.CandidateRepo, llmProvider llm.LLMProvider) *PositioningHandler {
	return &PositioningHandler{candidateRepo: candidateRepo, llm: llmProvider}
}

// positionRequest is the request body for POST /api/v1/jobs/position
type positionRequest struct {
	// Job info — can come from job search or manual paste
	JobTitle       string `json:"job_title" binding:"required"`
	Company        string `json:"company"`
	Location       string `json:"location"`
	JobDescription string `json:"job_description" binding:"required,min=50"`
	// Optional: declared skills from the candidate (comma-separated or array)
	DeclaredSkills []string `json:"declared_skills"`
}

// PositioningResult is returned to the frontend.
type PositioningResult struct {
	OverallMatch     int                `json:"overall_match"`      // 0-100
	TierFit          string             `json:"tier_fit"`           // "below" | "match" | "above"
	TierFitLabel     string             `json:"tier_fit_label"`
	CompanyBar       string             `json:"company_bar"`        // "high" | "medium" | "accessible"
	CompanyBarLabel  string             `json:"company_bar_label"`
	Summary          string             `json:"summary"`
	SkillMatches     []SkillSignal      `json:"skill_matches"`
	SkillGaps        []SkillSignal      `json:"skill_gaps"`
	ActionPlan       []ActionItem       `json:"action_plan"`
	InterviewFocus   []string           `json:"interview_focus"`
	TimeToReady      string             `json:"time_to_ready"`
	Confidence       string             `json:"confidence"`         // "high" | "medium" | "low"
}

type SkillSignal struct {
	Skill    string `json:"skill"`
	Level    string `json:"level"`    // "strong" | "partial" | "missing"
	Comment  string `json:"comment"`
}

type ActionItem struct {
	Priority string `json:"priority"` // "critical" | "high" | "medium"
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Resource string `json:"resource,omitempty"`
}

// Analyse godoc
// POST /api/v1/jobs/position
func (h *PositioningHandler) Analyse(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorEnvelope("UNAUTHORIZED", "Not authenticated"))
		return
	}

	var req positionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorEnvelope("VALIDATION_ERROR", err.Error()))
		return
	}

	// Load candidate profile
	cand, err := h.candidateRepo.GetByUserID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, errorEnvelope("NOT_FOUND", "Create your profile first"))
		return
	}

	// Build skills context
	skillsContext := ""
	if len(req.DeclaredSkills) > 0 {
		skillsContext = fmt.Sprintf("Candidate's declared skills: %s", strings.Join(req.DeclaredSkills, ", "))
	} else {
		skillsContext = "No explicit skills provided — infer from experience tier and company context."
	}

	systemPrompt := `You are an expert technical recruiter and career coach specialising in the Indian software engineering job market (FAANG, tier-1 Indian tech, startups).
You analyse a candidate's profile against a specific job description and return a precise, honest, and actionable positioning assessment.

Return ONLY valid JSON matching this exact schema — no markdown, no extra text:
{
  "overall_match": <integer 0-100>,
  "tier_fit": "<below|match|above>",
  "tier_fit_label": "<one sentence, e.g. 'Your 4 YOE matches SDE-2 level perfectly'>",
  "company_bar": "<high|medium|accessible>",
  "company_bar_label": "<one sentence about company difficulty level>",
  "summary": "<2-3 sentence honest positioning summary>",
  "skill_matches": [
    {"skill": "<skill name>", "level": "<strong|partial>", "comment": "<why this is a signal>"}
  ],
  "skill_gaps": [
    {"skill": "<skill name>", "level": "<partial|missing>", "comment": "<what's missing and why it matters>"}
  ],
  "action_plan": [
    {"priority": "<critical|high|medium>", "title": "<short action>", "detail": "<what to do specifically>", "resource": "<optional: book/course/topic to study>"}
  ],
  "interview_focus": ["<key topic 1>", "<key topic 2>", ...],
  "time_to_ready": "<e.g. '2-4 weeks of focused prep'>",
  "confidence": "<high|medium|low>"
}

Rules:
- overall_match: be honest. 40-60 is typical for stretch roles. 80+ means very strong.
- tier_fit: compare candidate YOE/tier to what the JD implies
- company_bar: "high" = FAANG/tier-1 (Google, Amazon, Meta, Microsoft), "medium" = funded Indian startups (Swiggy, Razorpay, CRED), "accessible" = other companies
- skill_gaps: include only skills that are EXPLICITLY required or strongly implied by the JD
- action_plan: max 5 items, ordered by priority. Be specific — not "study algorithms" but "practice DP on LeetCode for 2 weeks (Blind 75 list)"
- interview_focus: 3-5 topics likely to be tested based on the JD
- time_to_ready: realistic estimate given the gaps`

	userPrompt := fmt.Sprintf(`CANDIDATE PROFILE:
- Years of experience: %d
- Experience tier: %s (%s)
- Current company: %s
- %s

TARGET JOB:
- Title: %s
- Company: %s
- Location: %s

JOB DESCRIPTION:
%s`,
		cand.YearsExperience,
		cand.InferredTier.TierLabel(),
		cand.TierExplanation,
		orDefault(cand.CurrentCompany, "not specified"),
		skillsContext,
		req.JobTitle,
		orDefault(req.Company, "not specified"),
		orDefault(req.Location, "not specified"),
		truncate(req.JobDescription, 3000),
	)

	resp, err := h.llm.Generate(c.Request.Context(), llm.LLMRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		MaxTokens:    2500,
		Temperature:  0.3,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorEnvelope("LLM_ERROR", "Positioning analysis failed"))
		return
	}

	// Parse LLM JSON response
	var result PositioningResult
	raw := extractJSON(resp.Content)

	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		c.JSON(http.StatusInternalServerError, errorEnvelope("LLM_PARSE_ERROR", "Positioning analysis returned unexpected format — please try again"))
		return
	}

	c.JSON(http.StatusOK, result)
}

// extractJSON finds the first '{' ... last '}' in s, handling markdown fences.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	// Strip markdown code fences line by line
	lines := strings.Split(s, "\n")
	filtered := lines[:0]
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "```json" || trimmed == "```" {
			continue
		}
		filtered = append(filtered, l)
	}
	s = strings.TrimSpace(strings.Join(filtered, "\n"))
	// Find JSON object bounds
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n[truncated]"
}
