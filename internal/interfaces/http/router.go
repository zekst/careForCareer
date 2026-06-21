package http

import (
	"crypto/rsa"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"careergps/internal/interfaces/http/handlers"
	"careergps/internal/interfaces/http/middleware"
)

// SetupRouter wires all routes and returns the configured Gin engine.
func SetupRouter(
	authHandler         *handlers.AuthHandler,
	candidateHandler    *handlers.CandidateHandler,
	resumeHandler       *handlers.ResumeHandler,
	assessmentHandler   *handlers.AssessmentHandler,
	coachHandler        *handlers.CoachHandler,
	jobsHandler         *handlers.JobsHandler,
	positioningHandler  *handlers.PositioningHandler,
	prepHandler         *handlers.PrepHandler,
	pivotHandler        *handlers.PivotHandler,
	studentHandler      *handlers.StudentHandler,
	publicKey           *rsa.PublicKey,
) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(cors.New(cors.Config{
		// Reflect the request origin back so credentialed (Bearer) requests work
		AllowOriginFunc:  func(_ string) bool { return true },
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))
	r.Use(middleware.RequestID())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	v1 := r.Group("/api/v1")

	authGroup := v1.Group("/auth")
	{
		authGroup.POST("/register", authHandler.Register)
		authGroup.POST("/login", authHandler.Login)
		authGroup.POST("/refresh", authHandler.Refresh)
		authGroup.POST("/logout", authHandler.Logout)
	}

	authed := v1.Group("")
	authed.Use(middleware.JWT(publicKey))
	{
		// Candidate profile
		authed.GET("/candidate", candidateHandler.GetProfile)
		authed.POST("/candidate", candidateHandler.CreateProfile)
		authed.PUT("/candidate", candidateHandler.UpdateProfile)

		// Resumes
		authed.POST("/resumes", resumeHandler.Upload)
		authed.GET("/resumes/:id", resumeHandler.GetResume)

		// Assessments
		authed.GET("/assessments/:id", assessmentHandler.Get)
		authed.GET("/assessments/:id/readiness", assessmentHandler.GetReadiness)

		// Coach
		authed.POST("/coach/sessions", coachHandler.CreateSession)
		authed.GET("/coach/sessions/:id", coachHandler.GetSession)
		authed.GET("/coach/sessions/:id/stream", coachHandler.Stream)

		// Jobs
		authed.GET("/jobs/search", jobsHandler.Search)
		authed.GET("/jobs/suggested", jobsHandler.Suggested)
		authed.POST("/jobs/position", positioningHandler.Analyse)
		authed.POST("/jobs/prep-plan", prepHandler.GeneratePrepPlan)

		// JD-aware coach sessions
		authed.POST("/coach/jd-sessions", prepHandler.CreateJDSession)
		authed.GET("/coach/jd-sessions/:id/stream", prepHandler.StreamJD)

	}

	// Guest-accessible routes (no auth required) — student track, pivot, public job search
	open := v1.Group("")
	{
		open.POST("/student/assess", studentHandler.Assess)
		open.POST("/pivot/analyse", pivotHandler.Analyse)
	}

	return r
}
