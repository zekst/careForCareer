package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"careergps/internal/domain/candidate"
	"careergps/internal/infrastructure/postgres"
	"careergps/internal/interfaces/http/middleware"
)

// Job represents a normalized job listing returned to the frontend.
type Job struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Company     string `json:"company"`
	Location    string `json:"location"`
	ApplyURL    string `json:"apply_url"`
	Description string `json:"description"`
	PostedAt    string `json:"posted_at,omitempty"`
	Source      string `json:"source"`
}

// JobsHandler handles job search and suggestion requests.
type JobsHandler struct {
	apifyToken    string
	httpClient    *http.Client
	redisClient   *redis.Client
	candidateRepo *postgres.CandidateRepo
	log           *zap.Logger
}

func NewJobsHandler(redisClient *redis.Client, candidateRepo *postgres.CandidateRepo, apifyToken string, log *zap.Logger) *JobsHandler {
	return &JobsHandler{
		apifyToken:    apifyToken,
		httpClient:    &http.Client{Timeout: 125 * time.Second},
		redisClient:   redisClient,
		candidateRepo: candidateRepo,
		log:           log,
	}
}

// Search godoc
// GET /api/v1/jobs/search?q=backend+engineer&location=bangalore&limit=20
func (h *JobsHandler) Search(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	location := strings.TrimSpace(c.Query("location"))
	limitStr := c.DefaultQuery("limit", "20")

	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "query param 'q' is required"}})
		return
	}

	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	// Check Redis cache
	cacheKey := fmt.Sprintf("jobs:search:%s:%s:%d", strings.ToLower(q), strings.ToLower(location), limit)
	if h.redisClient != nil {
		if cached, err := h.redisClient.Get(c.Request.Context(), cacheKey).Bytes(); err == nil {
			var jobs []Job
			if json.Unmarshal(cached, &jobs) == nil {
				c.JSON(http.StatusOK, gin.H{"jobs": jobs, "total": len(jobs), "query": q, "location": location})
				return
			}
		}
	}

	var jobs []Job
	var err error
	var apifyErr string

	if h.apifyToken != "" {
		// bebity actor takes title and location as separate fields
		jobs, err = h.fetchFromApify(c.Request.Context(), q, location, limit)
		if err != nil {
			apifyErr = err.Error()
			h.log.Error("apify search fetch failed", zap.String("query", q), zap.Error(err))
		}
	} else {
		apifyErr = "token_not_set"
	}

	// Fallback to mock data if Apify fails, token not set, or returned zero results
	if err != nil || h.apifyToken == "" || len(jobs) == 0 {
		jobs = h.mockJobs(q, location, limit)
		if apifyErr == "" && h.apifyToken != "" {
			apifyErr = "zero_results_from_apify"
		}
	}

	// Cache results for 5 minutes (only real results, not mock fallback)
	if h.redisClient != nil && len(jobs) > 0 && apifyErr == "" {
		if b, jsonErr := json.Marshal(jobs); jsonErr == nil {
			h.redisClient.Set(c.Request.Context(), cacheKey, b, 5*time.Minute)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"jobs":       jobs,
		"total":      len(jobs),
		"query":      q,
		"location":   location,
		"_debug_err": apifyErr,
	})
}

// Suggested godoc
// GET /api/v1/jobs/suggested
// Returns jobs tailored to the authenticated candidate's experience tier and profile.
func (h *JobsHandler) Suggested(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorEnvelope("UNAUTHORIZED", "Not authenticated"))
		return
	}

	cand, err := h.candidateRepo.GetByUserID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, errorEnvelope("NOT_FOUND", "Create your profile first"))
		return
	}

	query, location := profileSearchQuery(cand)

	// Cache suggested results per user for 10 minutes
	cacheKey := fmt.Sprintf("jobs:suggested:%s", userID.String())
	if h.redisClient != nil {
		if cached, cErr := h.redisClient.Get(c.Request.Context(), cacheKey).Bytes(); cErr == nil {
			var jobs []Job
			if json.Unmarshal(cached, &jobs) == nil {
				c.JSON(http.StatusOK, gin.H{
					"jobs":     jobs,
					"total":    len(jobs),
					"query":    query,
					"location": location,
					"based_on": tierSummary(cand),
				})
				return
			}
		}
	}

	var jobs []Job
	var apifyErr string
	if h.apifyToken != "" {
		jobs, err = h.fetchFromApify(c.Request.Context(), query, location, 8)
		if err != nil {
			apifyErr = err.Error()
			h.log.Error("apify suggested fetch failed", zap.String("query", query), zap.Error(err))
		}
	} else {
		apifyErr = "token_not_set"
	}
	if err != nil || h.apifyToken == "" || len(jobs) == 0 {
		jobs = h.mockJobs(query, location, 8)
		if apifyErr == "" && h.apifyToken != "" {
			apifyErr = "zero_results_from_apify"
		}
	}

	// Cache for 10 minutes (only cache real results, not mock fallback)
	if h.redisClient != nil && len(jobs) > 0 && apifyErr == "" {
		if b, jsonErr := json.Marshal(jobs); jsonErr == nil {
			h.redisClient.Set(c.Request.Context(), cacheKey, b, 10*time.Minute)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"jobs":       jobs,
		"total":      len(jobs),
		"query":      query,
		"location":   location,
		"based_on":   tierSummary(cand),
		"_debug_err": apifyErr,
	})
}

// profileSearchQuery builds a targeted search query from the candidate's profile.
func profileSearchQuery(cand *candidate.Candidate) (query, location string) {
	switch cand.InferredTier {
	case candidate.TierFreshGrad:
		query = "software engineer fresher entry level"
	case candidate.TierJunior:
		query = "junior software engineer SDE-1 backend"
	case candidate.TierMidLevel:
		query = "software engineer SDE-2 backend golang python"
	case candidate.TierSenior:
		query = "senior software engineer SDE-3 backend distributed systems"
	case candidate.TierStaff:
		query = "staff engineer principal engineer backend"
	default:
		query = "software engineer backend"
	}
	return query, "India"
}

func tierSummary(cand *candidate.Candidate) string {
	base := fmt.Sprintf("%d YOE, %s", cand.YearsExperience, cand.InferredTier.TierLabel())
	if cand.CurrentCompany != "" {
		base += " at " + cand.CurrentCompany
	}
	return base
}

// fetchFromApify calls the Apify LinkedIn Jobs Scraper actor (bebity/linkedin-jobs-scraper).
// Input schema: title (keywords), location (string), rows (int, max 1000).
func (h *JobsHandler) fetchFromApify(ctx context.Context, query, location string, limit int) ([]Job, error) {
	const actorRef = "bebity~linkedin-jobs-scraper"

	if limit < 1 {
		limit = 10
	}
	payload := map[string]interface{}{
		"title":    query,
		"location": location,
		"rows":     limit,
	}

	payloadBytes, _ := json.Marshal(payload)

	// LinkedIn scraping typically takes 60-120 seconds; use 110s Apify timeout.
	runURL := fmt.Sprintf("https://api.apify.com/v2/acts/%s/run-sync-get-dataset-items?token=%s&timeout=110",
		actorRef, h.apifyToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, runURL, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apify request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("apify returned %d: %s", resp.StatusCode, snippet)
	}

	var raw []map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("apify response parse failed: %w", err)
	}

	jobs := make([]Job, 0, len(raw))
	for i, item := range raw {
		if i >= limit {
			break
		}
		id := getString(item, "id", "jobId", "job_id")
		if id == "" {
			id = fmt.Sprintf("linkedin-%d", i)
		}
		job := Job{
			ID:          id,
			Title:       getString(item, "title", "position", "jobTitle"),
			Company:     getString(item, "company", "companyName", "company_name"),
			Location:    getString(item, "location", "jobLocation"),
			ApplyURL:    getString(item, "url", "applyUrl", "jobUrl", "link"),
			Description: getString(item, "description", "descriptionText", "jobDescription"),
			PostedAt:    getString(item, "publishedAt", "postedAt", "datePosted"),
			Source:      "linkedin",
		}
		if job.Title != "" {
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

// mockJobs returns realistic-looking placeholder jobs for when Apify is unavailable.
func (h *JobsHandler) mockJobs(query, location string, limit int) []Job {
	loc := location
	if loc == "" {
		loc = "India"
	}

	templates := []Job{
		{
			ID:       "mock-1",
			Title:    fmt.Sprintf("Senior %s Engineer", titleCase(query)),
			Company:  "Amazon",
			Location: loc,
			ApplyURL: "https://www.linkedin.com/jobs/search/?keywords=" + url.QueryEscape(query),
			Description: fmt.Sprintf("We are looking for a Senior %s Engineer to join our growing team. You will design and implement scalable distributed systems, mentor junior engineers, and drive technical decisions.\n\nRequirements:\n- 5+ years of experience\n- Strong system design skills\n- Proficiency in Go, Java, or Python\n- Experience with AWS, microservices", titleCase(query)),
			Source:   "mock",
		},
		{
			ID:       "mock-2",
			Title:    fmt.Sprintf("%s Engineer II", titleCase(query)),
			Company:  "Google",
			Location: loc,
			ApplyURL: "https://www.linkedin.com/jobs/search/?keywords=" + url.QueryEscape(query),
			Description: fmt.Sprintf("%s Engineer II at Google. Join our team building products used by billions. You'll work on large-scale infrastructure and develop innovative solutions.\n\nRequirements:\n- 3+ years experience\n- Strong CS fundamentals\n- Coding proficiency in C++, Java, Go, or Python\n- Problem solving and system design skills", titleCase(query)),
			Source:   "mock",
		},
		{
			ID:       "mock-3",
			Title:    fmt.Sprintf("Staff %s Engineer", titleCase(query)),
			Company:  "Microsoft",
			Location: loc,
			ApplyURL: "https://www.linkedin.com/jobs/search/?keywords=" + url.QueryEscape(query),
			Description: fmt.Sprintf("Staff %s Engineer — Azure. Drive the technical strategy for our cloud platform. Own end-to-end delivery of complex systems.\n\nRequirements:\n- 8+ years of experience\n- Track record of leading large engineering projects\n- Deep expertise in distributed systems\n- Excellent communication skills", titleCase(query)),
			Source:   "mock",
		},
		{
			ID:       "mock-4",
			Title:    fmt.Sprintf("%s Developer (Backend)", titleCase(query)),
			Company:  "Flipkart",
			Location: loc,
			ApplyURL: "https://www.linkedin.com/jobs/search/?keywords=" + url.QueryEscape(query),
			Description: fmt.Sprintf("Backend %s Developer at Flipkart. Build high-performance APIs and data pipelines for India's largest e-commerce platform.\n\nRequired skills:\n- Go / Java / Python\n- Kafka, Redis, MySQL\n- REST API design\n- 2+ years experience", titleCase(query)),
			Source:   "mock",
		},
		{
			ID:       "mock-5",
			Title:    fmt.Sprintf("Principal %s Engineer", titleCase(query)),
			Company:  "Uber",
			Location: loc,
			ApplyURL: "https://www.linkedin.com/jobs/search/?keywords=" + url.QueryEscape(query),
			Description: fmt.Sprintf("Principal %s Engineer at Uber. Define the architecture for our real-time platform serving millions of rides daily.\n\nRequirements:\n- 10+ years of engineering experience\n- Deep expertise in distributed systems and databases\n- Proven technical leadership\n- Strong system design skills", titleCase(query)),
			Source:   "mock",
		},
		{
			ID:       "mock-6",
			Title:    fmt.Sprintf("SDE-2 (%s)", titleCase(query)),
			Company:  "Swiggy",
			Location: loc,
			ApplyURL: "https://www.linkedin.com/jobs/search/?keywords=" + url.QueryEscape(query),
			Description: fmt.Sprintf("Software Development Engineer-2 at Swiggy. Work on our food delivery platform scaling to millions of orders.\n\nSkills required:\n- 3-5 years experience\n- Java/Go/Python\n- Microservices, Docker, Kubernetes\n- SQL and NoSQL databases\n- Strong problem-solving skills"),
			Source:   "mock",
		},
		{
			ID:       "mock-7",
			Title:    fmt.Sprintf("Senior %s Engineer", titleCase(query)),
			Company:  "Meesho",
			Location: loc,
			ApplyURL: "https://www.linkedin.com/jobs/search/?keywords=" + url.QueryEscape(query),
			Description: fmt.Sprintf("Senior %s Engineer at Meesho. Help build the commerce infrastructure for 150M+ Indian entrepreneurs.\n\nWhat we're looking for:\n- 4-7 years experience\n- Strong backend development skills\n- Experience with high-traffic systems\n- Passion for impact at scale", titleCase(query)),
			Source:   "mock",
		},
		{
			ID:       "mock-8",
			Title:    fmt.Sprintf("%s Engineer - Payments", titleCase(query)),
			Company:  "Razorpay",
			Location: loc,
			ApplyURL: "https://www.linkedin.com/jobs/search/?keywords=" + url.QueryEscape(query),
			Description: fmt.Sprintf("%s Engineer on the Payments team at Razorpay. Build reliable, secure payment infrastructure processing billions of transactions.\n\nRequirements:\n- 3+ years backend experience\n- Strong understanding of distributed systems\n- Knowledge of payment protocols a plus\n- Go, Java, or Python", titleCase(query)),
			Source:   "mock",
		},
	}

	if limit > len(templates) {
		limit = len(templates)
	}
	return templates[:limit]
}

func getString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func titleCase(s string) string {
	if s == "" {
		return "Software"
	}
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
