package pathwayai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_DisabledIsNoOp(t *testing.T) {
	// Empty base URL => disabled => Recommend is a graceful no-op.
	c := New("", "careforcareer")
	if c.Enabled() {
		t.Fatal("client with empty base URL should be disabled")
	}
	recs, err := c.Recommend(context.Background(), []GapInput{{Competency: "Kubernetes"}}, "ctx")
	if err != nil || recs != nil {
		t.Fatalf("disabled client should return (nil, nil); got (%v, %v)", recs, err)
	}
}

func TestClient_NoGapsIsNoOp(t *testing.T) {
	c := New("http://example.invalid", "careforcareer")
	recs, err := c.Recommend(context.Background(), nil, "ctx")
	if err != nil || recs != nil {
		t.Fatalf("no gaps should return (nil, nil); got (%v, %v)", recs, err)
	}
}

func TestClient_RecommendRoundTrip(t *testing.T) {
	var gotPath string
	var gotBody recommendRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(recommendResponse{
			Namespace: "careforcareer",
			Recommendations: []Recommendation{{
				ContentID:    "k8s_course",
				Title:        "Kubernetes Fundamentals",
				AddressesGap: "Kubernetes",
				Rationale:    "Covers pods and deployments.",
				SourceChunks: []SourceChunk{{ChunkID: "k8s__chunk_0000", DocumentID: "k8s_course", Text: "pods…", Score: 0.9}},
			}},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "careforcareer")
	recs, err := c.Recommend(context.Background(), []GapInput{{Competency: "Kubernetes"}}, "targeting SRE")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Correct namespaced path.
	if gotPath != "/v1/careforcareer/recommend" {
		t.Errorf("path = %q, want /v1/careforcareer/recommend", gotPath)
	}
	// Gap defaults applied: required_level defaults to 1.0, score = required - current.
	if len(gotBody.Gaps) != 1 || gotBody.Gaps[0].RequiredLevel != 1.0 || gotBody.Gaps[0].Score != 1.0 {
		t.Errorf("gap defaults not applied: %+v", gotBody.Gaps)
	}
	// Response decoded, provenance preserved.
	if len(recs) != 1 || recs[0].Title != "Kubernetes Fundamentals" {
		t.Fatalf("unexpected recommendations: %+v", recs)
	}
	if len(recs[0].SourceChunks) != 1 || recs[0].SourceChunks[0].ChunkID != "k8s__chunk_0000" {
		t.Errorf("source chunks (provenance) not preserved: %+v", recs[0].SourceChunks)
	}
}

func TestClient_RecommendNon200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "careforcareer")
	_, err := c.Recommend(context.Background(), []GapInput{{Competency: "X"}}, "")
	if err == nil {
		t.Fatal("expected error on non-200 response")
	}
}
