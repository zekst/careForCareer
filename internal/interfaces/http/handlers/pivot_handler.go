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

// PivotHandler analyses a career pivot from current role to a target role.
type PivotHandler struct {
	candidateRepo *postgres.CandidateRepo
	llm           llm.LLMProvider
}

func NewPivotHandler(candidateRepo *postgres.CandidateRepo, llmProvider llm.LLMProvider) *PivotHandler {
	return &PivotHandler{candidateRepo: candidateRepo, llm: llmProvider}
}

// pivotRequest is the body for POST /api/v1/pivot/analyse
type pivotRequest struct {
	// What the candidate is pivoting TO
	TargetRole     string `json:"target_role" binding:"required,max=100"` // e.g. "Program Manager"
	TargetCompany  string `json:"target_company"`
	CompanyType    string `json:"company_type"`                           // "faang" | "startup" | "mid-size" | "any"
	// Optional JD — if provided, analysis is grounded in a real posting
	JobDescription string `json:"job_description" binding:"omitempty,max=20000"`
	// Candidate's current context (supplements profile)
	CurrentRole   string   `json:"current_role" binding:"omitempty,max=200"` // e.g. "SDE-2 at Amazon"
	CurrentSkills []string `json:"current_skills"`                           // from resume parse or manual
}

// ── Response types ────────────────────────────────────────────────────────────

type PivotResult struct {
	TargetRole       string            `json:"target_role"`
	PivotDifficulty  string            `json:"pivot_difficulty"`   // "hard" | "moderate" | "easy"
	DifficultyLabel  string            `json:"difficulty_label"`
	OverallFit       int               `json:"overall_fit"`         // 0-100 starting point
	Summary          string            `json:"summary"`             // honest 2-3 sentence assessment
	TransferableSkills []PivotSkill    `json:"transferable_skills"`
	SkillsToLearn    []PivotSkill      `json:"skills_to_learn"`
	Timeline         PivotTimeline     `json:"timeline"`
	EntryPaths       []EntryPath       `json:"entry_paths"`
	PrepPlanOutline  []PivotWeek       `json:"prep_plan_outline"`
	DayInTheLife     string            `json:"day_in_the_life"`     // what the target role actually does
	HonestCaveats    []string          `json:"honest_caveats"`      // things that could go wrong
	QuickWins        []string          `json:"quick_wins"`          // things to do in the next 2 weeks
}

type PivotSkill struct {
	Skill      string `json:"skill"`
	Relevance  string `json:"relevance"`   // "high" | "medium" | "low"
	Comment    string `json:"comment"`
}

type PivotTimeline struct {
	Optimistic  string `json:"optimistic"`   // e.g. "4-6 months"
	Realistic   string `json:"realistic"`    // e.g. "8-12 months"
	WhatHelps   string `json:"what_helps"`   // what makes it faster
	WhatHurts   string `json:"what_hurts"`   // what makes it slower
}

type EntryPath struct {
	Name        string `json:"name"`        // e.g. "APM Program", "Internal Transfer"
	Difficulty  string `json:"difficulty"`  // "high" | "medium" | "low"
	Description string `json:"description"`
	Examples    []string `json:"examples"` // e.g. ["Google APM", "Microsoft PM Explore"]
	BestFor     string `json:"best_for"`
}

type PivotWeek struct {
	Phase   string   `json:"phase"`    // e.g. "Foundation", "Deep Dive", "Apply"
	Weeks   string   `json:"weeks"`    // e.g. "1-3"
	Topics  []string `json:"topics"`
	Goal    string   `json:"goal"`
}

// Analyse godoc
// POST /api/v1/pivot/analyse
func (h *PivotHandler) Analyse(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorEnvelope("UNAUTHORIZED", "Not authenticated"))
		return
	}

	var req pivotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorEnvelope("VALIDATION_ERROR", err.Error()))
		return
	}

	// Load candidate profile for YOE + tier context
	cand, err := h.candidateRepo.GetByUserID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, errorEnvelope("NOT_FOUND", "Create your profile first"))
		return
	}

	currentSkillsStr := ""
	if len(req.CurrentSkills) > 0 {
		currentSkillsStr = "Current skills: " + strings.Join(req.CurrentSkills, ", ")
	}

	companyCtx := req.CompanyType
	if req.TargetCompany != "" {
		companyCtx = req.TargetCompany
	}
	if companyCtx == "" {
		companyCtx = "any company"
	}

	jdSection := ""
	if req.JobDescription != "" {
		jd := req.JobDescription
		if len(jd) > 2000 {
			jd = jd[:2000] + "\n[truncated]"
		}
		jdSection = "\n\nJOB DESCRIPTION (specific posting):\n" + jd
	}

	systemPrompt := `You are an expert career coach specialising in role transitions for software engineers in India and globally.
You give honest, realistic, specific advice about pivoting from engineering to roles like PM, TPM, EM, SRE, Data Science, etc.

Return ONLY valid JSON matching this exact schema — no markdown, no extra text:
{
  "target_role": "<role name>",
  "pivot_difficulty": "<hard|moderate|easy>",
  "difficulty_label": "<one sentence e.g. 'This is a significant pivot — most engineers take 6-12 months'>",
  "overall_fit": <integer 0-100, starting-point fit before any prep>,
  "summary": "<2-3 honest sentences: is this realistic, what are the main bridges and gaps>",
  "transferable_skills": [
    {"skill": "<skill>", "relevance": "<high|medium|low>", "comment": "<how it transfers to the target role>"}
  ],
  "skills_to_learn": [
    {"skill": "<skill>", "relevance": "<high|medium>", "comment": "<why it's needed, how different it is from engineering>"}
  ],
  "timeline": {
    "optimistic": "<e.g. '4-6 months'>",
    "realistic": "<e.g. '8-12 months'>",
    "what_helps": "<one sentence>",
    "what_hurts": "<one sentence>"
  },
  "entry_paths": [
    {
      "name": "<path name>",
      "difficulty": "<high|medium|low>",
      "description": "<what this path involves>",
      "examples": ["<example1>", "<example2>"],
      "best_for": "<who this path suits>"
    }
  ],
  "prep_plan_outline": [
    {
      "phase": "<phase name>",
      "weeks": "<e.g. '1-4'>",
      "topics": ["<topic1>", "<topic2>"],
      "goal": "<what to achieve in this phase>"
    }
  ],
  "day_in_the_life": "<2-3 sentences describing what a typical day actually looks like in the target role — be honest about what's less technical>",
  "honest_caveats": ["<caveat1>", "<caveat2>"],
  "quick_wins": ["<action1 to do in the next 2 weeks>", "<action2>", "<action3>"]
}

Rules:
- overall_fit: reflect realistic starting point. A strong SDE pivoting to PM might start at 40-55. To SRE: 65-75. To EM: 55-70.
- transferable_skills: be generous but honest — deep technical background IS valuable in PM/TPM/EM
- skills_to_learn: focus on the genuinely new mindset shifts, not just tools
- entry_paths: include at minimum — internal transfer, APM/RPM programs (if < 5 YOE), direct application, TPM as a bridge
- prep_plan_outline: 3-4 phases, realistic week ranges based on timeline
- honest_caveats: say the things most coaches don't — e.g. "PM roles get fewer interview calls for candidates without PM title on resume"
- quick_wins: actionable things to start immediately — reading a book, joining a community, doing a side project
- day_in_the_life: be honest that PM work is more meetings, stakeholder management, ambiguity — not coding`

	userPrompt := fmt.Sprintf(`CANDIDATE:
- Years of experience: %d
- Tier: %s
- Current company: %s
- Current role context: %s
- %s

PIVOT TARGET:
- Target role: %s
- Company context: %s%s`,
		cand.YearsExperience,
		cand.InferredTier.TierLabel(),
		orDefault(cand.CurrentCompany, "not specified"),
		orDefault(req.CurrentRole, fmt.Sprintf("SDE at %s", orDefault(cand.CurrentCompany, "a tech company"))),
		orDefault(currentSkillsStr, "No specific skills listed — infer from experience tier"),
		req.TargetRole,
		companyCtx,
		jdSection,
	)

	resp, err := h.llm.Generate(c.Request.Context(), llm.LLMRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		MaxTokens:    2500,
		Temperature:  0.3,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorEnvelope("LLM_ERROR", "Pivot analysis failed"))
		return
	}

	raw := strings.TrimSpace(resp.Content)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var result PivotResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		c.JSON(http.StatusInternalServerError, errorEnvelope("PARSE_ERROR", "Failed to parse pivot analysis"))
		return
	}

	c.JSON(http.StatusOK, result)
}
