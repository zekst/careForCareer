package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"careergps/internal/application/auth"
	"careergps/pkg/apperrors"
)

type AuthHandler struct {
	authSvc *auth.Service
}

func NewAuthHandler(authSvc *auth.Service) *AuthHandler {
	return &AuthHandler{authSvc: authSvc}
}

type registerRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorEnvelope("VALIDATION_ERROR", err.Error()))
		return
	}
	user, tokens, err := h.authSvc.Register(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, apperrors.ErrConflict) {
			c.JSON(http.StatusConflict, errorEnvelope("EMAIL_TAKEN", "An account with this email already exists"))
			return
		}
		c.JSON(http.StatusInternalServerError, errorEnvelope("INTERNAL_ERROR", "Registration failed"))
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"user_id":       user.ID,
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"expires_in":    tokens.ExpiresIn,
	})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorEnvelope("VALIDATION_ERROR", err.Error()))
		return
	}
	tokens, err := h.authSvc.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, apperrors.ErrUnauthorized) {
			c.JSON(http.StatusUnauthorized, errorEnvelope("INVALID_CREDENTIALS", "Email or password is incorrect"))
			return
		}
		c.JSON(http.StatusInternalServerError, errorEnvelope("INTERNAL_ERROR", "Login failed"))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user_id":       tokens.UserID,
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"expires_in":    tokens.ExpiresIn,
	})
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorEnvelope("VALIDATION_ERROR", err.Error()))
		return
	}
	tokens, err := h.authSvc.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, errorEnvelope("INVALID_TOKEN", "Refresh token is invalid or expired"))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user_id":       tokens.UserID,
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"expires_in":    tokens.ExpiresIn,
	})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	var req logoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorEnvelope("VALIDATION_ERROR", err.Error()))
		return
	}
	if err := h.authSvc.Logout(c.Request.Context(), req.RefreshToken); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		c.JSON(http.StatusInternalServerError, errorEnvelope("INTERNAL_ERROR", "Logout failed"))
		return
	}
	c.Status(http.StatusNoContent)
}

func errorEnvelope(code, message string) gin.H {
	return gin.H{
		"error": gin.H{
			"code":    code,
			"message": message,
		},
	}
}
