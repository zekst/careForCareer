// Package pathwayai is careForCareer's client for the standalone PathwayAI
// skill-intelligence service (github.com/zekst/pathwayai).
//
// PathwayAI turns a set of skill gaps into grounded, *cited* learning
// recommendations retrieved from an ingested content corpus — unlike an LLM
// that invents resource names, every recommendation traces back to real source
// chunks. careForCareer uses it to enrich the prep plan with a verifiable
// "recommended resources" block.
//
// The integration is deliberately optional: if the service is not configured or
// is unreachable, callers treat it as a no-op and the existing flow is
// unaffected. That keeps PathwayAI a value-add, never a hard dependency.
package pathwayai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// requestTimeout bounds a single /recommend call so a slow or down PathwayAI
// never stalls the prep-plan response beyond this.
const requestTimeout = 10 * time.Second

// Client talks to a PathwayAI deployment for a single namespace (tenant).
type Client struct {
	baseURL   string
	namespace string
	http      *http.Client
}

// New builds a client. When baseURL is empty the client is disabled: every call
// is a graceful no-op (see Enabled). Callers can construct unconditionally and
// let the client decide, so wiring code stays branch-free.
func New(baseURL, namespace string) *Client {
	if namespace == "" {
		namespace = "careforcareer"
	}
	return &Client{
		baseURL:   baseURL,
		namespace: namespace,
		http:      &http.Client{Timeout: requestTimeout},
	}
}

// Enabled reports whether the client is configured to make calls.
func (c *Client) Enabled() bool {
	return c != nil && c.baseURL != ""
}

// GapInput is a skill gap to close. Maps to PathwayAI's Gap schema. careForCareer
// often only has the competency name (from string skill gaps); CurrentLevel /
// RequiredLevel / Score default sensibly for that case.
type GapInput struct {
	Competency    string
	CurrentLevel  float64
	RequiredLevel float64
	Score         float64
}

// SourceChunk is one piece of corpus evidence backing a recommendation (JD #11
// provenance): the exact chunk, its origin document, and its relevance score.
type SourceChunk struct {
	ChunkID    string  `json:"chunk_id"`
	DocumentID string  `json:"document_id"`
	Text       string  `json:"text"`
	Score      float64 `json:"score"`
}

// Recommendation is one grounded content suggestion for a gap.
type Recommendation struct {
	ContentID    string        `json:"content_id"`
	Title        string        `json:"title"`
	AddressesGap string        `json:"addresses_gap"`
	Rationale    string        `json:"rationale"`
	SourceChunks []SourceChunk `json:"source_chunks"`
}

// --- wire types matching PathwayAI's JSON contract -------------------------

type wireProvenance struct {
	Sources []SourceChunk `json:"sources"`
	Note    string        `json:"note"`
}

type wireGap struct {
	Competency    string         `json:"competency"`
	CurrentLevel  float64        `json:"current_level"`
	RequiredLevel float64        `json:"required_level"`
	Score         float64        `json:"score"`
	Evidence      []any          `json:"evidence"`
	Provenance    wireProvenance `json:"provenance"`
}

type recommendRequest struct {
	Gaps           []wireGap `json:"gaps"`
	ProfileContext string    `json:"profile_context,omitempty"`
}

type recommendResponse struct {
	Namespace       string           `json:"namespace"`
	Recommendations []Recommendation `json:"recommendations"`
}

// Recommend returns grounded, cited content for the given gaps.
//
// It is safe to call when disabled (returns nil, nil). Errors are returned so
// the caller can decide how to degrade; the prep handler treats them as
// non-fatal and simply omits the grounded-resources block.
func (c *Client) Recommend(ctx context.Context, gaps []GapInput, profileContext string) ([]Recommendation, error) {
	if !c.Enabled() || len(gaps) == 0 {
		return nil, nil
	}

	wireGaps := make([]wireGap, 0, len(gaps))
	for _, g := range gaps {
		required := g.RequiredLevel
		if required == 0 {
			required = 1.0 // default: the role fully requires this competency
		}
		score := g.Score
		if score == 0 {
			score = required - g.CurrentLevel
		}
		wireGaps = append(wireGaps, wireGap{
			Competency:    g.Competency,
			CurrentLevel:  g.CurrentLevel,
			RequiredLevel: required,
			Score:         score,
			Evidence:      []any{},
			Provenance:    wireProvenance{Sources: []SourceChunk{}, Note: "from careForCareer prep-plan gaps"},
		})
	}

	body, err := json.Marshal(recommendRequest{Gaps: wireGaps, ProfileContext: profileContext})
	if err != nil {
		return nil, fmt.Errorf("pathwayai: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/%s/recommend", c.baseURL, c.namespace)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("pathwayai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pathwayai: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pathwayai: unexpected status %d", resp.StatusCode)
	}

	var out recommendResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("pathwayai: decode response: %w", err)
	}
	return out.Recommendations, nil
}
