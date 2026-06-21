package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"careergps/internal/domain/resume"
	"careergps/internal/infrastructure/postgres"
	s3infra "careergps/internal/infrastructure/s3"
	"careergps/internal/interfaces/http/middleware"
	"careergps/pkg/apperrors"
)

type ResumeHandler struct {
	resumeRepo    *postgres.ResumeRepo
	candidateRepo *postgres.CandidateRepo
	storage       *s3infra.Storage
}

func NewResumeHandler(
	resumeRepo *postgres.ResumeRepo,
	candidateRepo *postgres.CandidateRepo,
	storage *s3infra.Storage,
) *ResumeHandler {
	return &ResumeHandler{
		resumeRepo:    resumeRepo,
		candidateRepo: candidateRepo,
		storage:       storage,
	}
}

// Upload accepts a PDF file upload, stores it in S3/MinIO, and creates a resume record.
func (h *ResumeHandler) Upload(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorEnvelope("UNAUTHORIZED", "Not authenticated"))
		return
	}

	// Candidate must exist first
	cand, err := h.candidateRepo.GetByUserID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			c.JSON(http.StatusBadRequest, errorEnvelope("PROFILE_REQUIRED", "Create a candidate profile first via POST /api/v1/candidate"))
			return
		}
		c.JSON(http.StatusInternalServerError, errorEnvelope("INTERNAL_ERROR", "Failed to fetch profile"))
		return
	}

	file, header, err := c.Request.FormFile("resume")
	if err != nil {
		c.JSON(http.StatusBadRequest, errorEnvelope("FILE_REQUIRED", "Attach a PDF file with field name 'resume'"))
		return
	}
	defer file.Close()

	if header.Size == 0 {
		c.JSON(http.StatusBadRequest, errorEnvelope("EMPTY_FILE", "Uploaded file is empty"))
		return
	}
	if header.Size > 5*1024*1024 {
		c.JSON(http.StatusBadRequest, errorEnvelope("FILE_TOO_LARGE", "Resume must be under 5 MB"))
		return
	}
	ct := header.Header.Get("Content-Type")
	if ct != "application/pdf" && ct != "application/octet-stream" {
		c.JSON(http.StatusBadRequest, errorEnvelope("INVALID_FILE_TYPE", "Only PDF files are accepted"))
		return
	}

	// Get next version number
	version, err := h.resumeRepo.NextVersion(c.Request.Context(), cand.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorEnvelope("INTERNAL_ERROR", "Failed to determine resume version"))
		return
	}

	resumeID := uuid.New()
	storageKey := s3infra.ResumeKey(cand.ID.String(), resumeID.String())

	// Upload to S3/MinIO
	if err := h.storage.Upload(c.Request.Context(), storageKey, file, header.Size); err != nil {
		c.JSON(http.StatusInternalServerError, errorEnvelope("UPLOAD_FAILED", "Failed to upload resume"))
		return
	}

	// Create resume record
	res, err := resume.New(cand.ID, resume.SourcePDF, storageKey, "", version)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorEnvelope("INTERNAL_ERROR", "Failed to create resume record"))
		return
	}
	res.ID = resumeID

	if err := h.resumeRepo.Create(c.Request.Context(), res); err != nil {
		c.JSON(http.StatusInternalServerError, errorEnvelope("INTERNAL_ERROR", "Failed to save resume record"))
		return
	}

	// Get a presigned download URL
	presignURL, err := h.storage.GetPresignedURL(c.Request.Context(), storageKey)
	if err != nil {
		// Non-fatal — resume is saved, URL is optional in response
		presignURL = ""
	}

	c.JSON(http.StatusCreated, gin.H{
		"resume_id":          res.ID,
		"candidate_id":       res.CandidateID,
		"version":            res.Version,
		"storage_key":        res.StorageKey,
		"extraction_status":  res.ExtractionStatus,
		"download_url":       presignURL,
		"created_at":         res.CreatedAt,
	})
}

// GetResume returns a resume record with a fresh presigned download URL.
func (h *ResumeHandler) GetResume(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorEnvelope("UNAUTHORIZED", "Not authenticated"))
		return
	}

	resumeID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errorEnvelope("INVALID_UUID", "Invalid resume ID"))
		return
	}

	res, err := h.resumeRepo.GetByID(c.Request.Context(), resumeID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			c.JSON(http.StatusNotFound, errorEnvelope("NOT_FOUND", "Resume not found"))
			return
		}
		c.JSON(http.StatusInternalServerError, errorEnvelope("INTERNAL_ERROR", "Failed to fetch resume"))
		return
	}

	// Enforce ownership — resume must belong to this user's candidate
	cand, err := h.candidateRepo.GetByUserID(c.Request.Context(), userID)
	if err != nil || cand.ID != res.CandidateID {
		c.JSON(http.StatusForbidden, errorEnvelope("FORBIDDEN", "Access denied"))
		return
	}

	presignURL, _ := h.storage.GetPresignedURL(c.Request.Context(), res.StorageKey)

	c.JSON(http.StatusOK, gin.H{
		"resume_id":         res.ID,
		"candidate_id":      res.CandidateID,
		"version":           res.Version,
		"source_type":       res.SourceType,
		"extraction_status": res.ExtractionStatus,
		"download_url":      presignURL,
		"created_at":        res.CreatedAt,
	})
}
