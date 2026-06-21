package main

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"careergps/config"
	"careergps/internal/application/auth"
	coachapp "careergps/internal/application/coach"
	"careergps/internal/infrastructure/llm"
	"careergps/internal/infrastructure/postgres"
	redisinfra "careergps/internal/infrastructure/redis"
	s3infra "careergps/internal/infrastructure/s3"
	"careergps/internal/interfaces/http/handlers"
	httpserver "careergps/internal/interfaces/http"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load failed: %v\n", err)
		os.Exit(1)
	}

	log := buildLogger(cfg.LogLevel)
	defer log.Sync()

	ctx := context.Background()

	// Database
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal("postgres connect failed", zap.Error(err))
	}
	defer pool.Close()

	// Redis
	redisClient, err := redisinfra.NewClient(cfg.RedisAddr, cfg.RedisPassword)
	if err != nil {
		log.Fatal("redis connect failed", zap.Error(err))
	}

	// S3
	storage, err := s3infra.New(ctx, cfg.S3Endpoint, cfg.S3Region, cfg.S3Bucket, cfg.AWSAccessKeyID, cfg.AWSSecretKey)
	if err != nil {
		log.Fatal("s3 init failed", zap.Error(err))
	}

	// JWT keys — prefer base64 env vars (production), fall back to file paths (local dev)
	privateKey, publicKey, err := loadRSAKeys(cfg.JWTPrivateKeyPath, cfg.JWTPublicKeyPath, cfg.JWTPrivateKeyB64, cfg.JWTPublicKeyB64)
	if err != nil {
		log.Fatal("jwt keys load failed", zap.Error(err))
	}

	// Repos
	userRepo := postgres.NewUserRepo(pool)
	candidateRepo := postgres.NewCandidateRepo(pool)
	resumeRepo := postgres.NewResumeRepo(pool)
	readinessRepo := postgres.NewReadinessRepo(pool)
	coachSessionRepo := postgres.NewCoachSessionRepo(pool)
	coachMessageRepo := postgres.NewCoachMessageRepo(pool)
	gapRepo := postgres.NewGapAnalysisRepo(pool)

	// Auth
	sessionRepo := postgres.NewSessionRepo(pool)
	authSvc := auth.NewService(userRepo, sessionRepo, privateKey, publicKey,
		cfg.AccessTokenTTLMin, cfg.RefreshTokenTTLDays)

	// LLM
	redisCache := redisinfra.NewCache(redisClient)
	ttl := time.Duration(cfg.LLMCacheTTLHours) * time.Hour
	anthropicProvider := llm.NewAnthropicProvider(cfg.AnthropicAPIKey, "")
	cachedLLM := llm.NewCachingProvider(anthropicProvider, redisCache, ttl, cfg.ScoringEngineVersion)

	// Coach
	dailyCounter := redisinfra.NewCoachDailyCounter(redisClient, cfg.CoachDailyLimit)
	roadmapRepo := postgres.NewRoadmapRepo(pool)
	coachSvc := coachapp.NewService(
		coachSessionRepo, coachMessageRepo,
		gapRepo, readinessRepo, roadmapRepo,
		cachedLLM, dailyCounter, cfg.CoachDailyLimit,
	)

	// Handlers
	authHandler := handlers.NewAuthHandler(authSvc)
	candidateHandler := handlers.NewCandidateHandler(candidateRepo)
	resumeHandler := handlers.NewResumeHandler(resumeRepo, candidateRepo, storage)
	assessmentHandler := handlers.NewAssessmentHandler(readinessRepo)
	coachHandler := handlers.NewCoachHandler(coachSvc, candidateRepo)
	jobsHandler := handlers.NewJobsHandler(redisClient, candidateRepo, cfg.ApifyAPIToken)
	positioningHandler := handlers.NewPositioningHandler(candidateRepo, cachedLLM)
	prepHandler := handlers.NewPrepHandler(coachSvc, candidateRepo, cachedLLM)
	pivotHandler := handlers.NewPivotHandler(candidateRepo, cachedLLM)
	studentHandler := handlers.NewStudentHandler(cachedLLM)

	// Router
	router := httpserver.SetupRouter(authHandler, candidateHandler, resumeHandler, assessmentHandler, coachHandler, jobsHandler, positioningHandler, prepHandler, pivotHandler, studentHandler, publicKey)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.APIPort),
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second, // SSE connections need longer write timeout
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		log.Info("api server starting", zap.Int("port", cfg.APIPort))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("server error", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutting down gracefully")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown error", zap.Error(err))
	}
	log.Info("server stopped")
}

func buildLogger(level string) *zap.Logger {
	lvl := zapcore.InfoLevel
	_ = lvl.UnmarshalText([]byte(level))

	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(lvl)
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	log, _ := cfg.Build()
	return log
}

func loadRSAKeys(privatePath, publicPath, privateB64, publicB64 string) (*rsa.PrivateKey, *rsa.PublicKey, error) {
	var privBytes, pubBytes []byte

	// Prefer base64 env vars (production/Render), fall back to file paths (local dev)
	if privateB64 != "" && publicB64 != "" {
		var err error
		privBytes, err = base64.StdEncoding.DecodeString(privateB64)
		if err != nil {
			return nil, nil, fmt.Errorf("decode private key base64: %w", err)
		}
		pubBytes, err = base64.StdEncoding.DecodeString(publicB64)
		if err != nil {
			return nil, nil, fmt.Errorf("decode public key base64: %w", err)
		}
	} else {
		var err error
		privBytes, err = os.ReadFile(privatePath)
		if err != nil {
			return nil, nil, fmt.Errorf("read private key: %w", err)
		}
		pubBytes, err = os.ReadFile(publicPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read public key: %w", err)
		}
	}

	block, _ := pem.Decode(privBytes)
	if block == nil {
		return nil, nil, fmt.Errorf("invalid private key PEM")
	}
	var privKey *rsa.PrivateKey
	if parsed, err2 := x509.ParsePKCS8PrivateKey(block.Bytes); err2 == nil {
		var ok bool
		privKey, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, nil, fmt.Errorf("private key is not RSA")
		}
	} else if parsed1, err1 := x509.ParsePKCS1PrivateKey(block.Bytes); err1 == nil {
		privKey = parsed1
	} else {
		return nil, nil, fmt.Errorf("parse private key: %w", err2)
	}

	pubBlock, _ := pem.Decode(pubBytes)
	if pubBlock == nil {
		return nil, nil, fmt.Errorf("invalid public key PEM")
	}
	pubInterface, err := x509.ParsePKIXPublicKey(pubBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse public key: %w", err)
	}
	pubKey, ok := pubInterface.(*rsa.PublicKey)
	if !ok {
		return nil, nil, fmt.Errorf("not an RSA public key")
	}

	return privKey, pubKey, nil
}
