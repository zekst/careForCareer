package config

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"

	"careergps/internal/domain/candidate"
	"careergps/internal/domain/readiness"
)

// Config holds all runtime configuration. Loaded from environment + .env file.
type Config struct {
	AppEnv  string `mapstructure:"APP_ENV"`
	APIPort int    `mapstructure:"API_PORT"`

	DatabaseURL string `mapstructure:"DATABASE_URL"`

	RedisAddr     string `mapstructure:"REDIS_ADDR"`
	RedisPassword string `mapstructure:"REDIS_PASSWORD"`

	S3Endpoint        string `mapstructure:"S3_ENDPOINT"`
	S3Bucket          string `mapstructure:"S3_BUCKET"`
	S3Region          string `mapstructure:"S3_REGION"`
	AWSAccessKeyID    string `mapstructure:"AWS_ACCESS_KEY_ID"`
	AWSSecretKey      string `mapstructure:"AWS_SECRET_ACCESS_KEY"`

	JWTPrivateKeyPath    string `mapstructure:"JWT_PRIVATE_KEY_PATH"`
	JWTPublicKeyPath     string `mapstructure:"JWT_PUBLIC_KEY_PATH"`
	JWTPrivateKeyB64     string `mapstructure:"JWT_PRIVATE_KEY_B64"`
	JWTPublicKeyB64      string `mapstructure:"JWT_PUBLIC_KEY_B64"`
	AccessTokenTTLMin    int    `mapstructure:"ACCESS_TOKEN_TTL_MINUTES"`
	RefreshTokenTTLDays  int    `mapstructure:"REFRESH_TOKEN_TTL_DAYS"`

	AnthropicAPIKey  string `mapstructure:"ANTHROPIC_API_KEY"`
	ApifyAPIToken    string `mapstructure:"APIFY_API_TOKEN"`
	OpenAIAPIKey     string `mapstructure:"OPENAI_API_KEY"`
	GeminiAPIKey     string `mapstructure:"GEMINI_API_KEY"`
	LLMPrimary       string `mapstructure:"LLM_PRIMARY_PROVIDER"`
	LLMCacheTTLHours int    `mapstructure:"LLM_CACHE_TTL_HOURS"`

	CoachDailyLimit           int `mapstructure:"COACH_DAILY_LIMIT"`
	RateLimitUnauthenticated  int `mapstructure:"RATE_LIMIT_UNAUTHENTICATED"`
	RateLimitAuthenticated    int `mapstructure:"RATE_LIMIT_AUTHENTICATED"`

	ScoringWeightsPath  string `mapstructure:"SCORING_WEIGHTS_PATH"`
	ScoringEngineVersion string `mapstructure:"SCORING_ENGINE_VERSION"`

	PrometheusPort int    `mapstructure:"OTEL_EXPORTER_PROMETHEUS_PORT"`
	LogLevel       string `mapstructure:"LOG_LEVEL"`

	// Loaded separately from SCORING_WEIGHTS_PATH
	WeightTable map[candidate.ExperienceTier]readiness.TierWeights
}

func Load() (*Config, error) {
	v := viper.New()
	v.SetConfigFile(".env")
	v.SetConfigType("env")
	v.AutomaticEnv()

	// Defaults
	v.SetDefault("APP_ENV", "development")
	v.SetDefault("API_PORT", 8080)
	v.SetDefault("ACCESS_TOKEN_TTL_MINUTES", 15)
	v.SetDefault("REFRESH_TOKEN_TTL_DAYS", 7)
	v.SetDefault("COACH_DAILY_LIMIT", 20)
	v.SetDefault("RATE_LIMIT_UNAUTHENTICATED", 30)
	v.SetDefault("RATE_LIMIT_AUTHENTICATED", 100)
	v.SetDefault("LLM_PRIMARY_PROVIDER", "anthropic")
	v.SetDefault("LLM_CACHE_TTL_HOURS", 24)
	v.SetDefault("SCORING_WEIGHTS_PATH", "./config/scoring_weights.yaml")
	v.SetDefault("SCORING_ENGINE_VERSION", "v1.0.0")
	v.SetDefault("OTEL_EXPORTER_PROMETHEUS_PORT", 9090)
	v.SetDefault("LOG_LEVEL", "info")

	_ = v.ReadInConfig() // .env is optional — env vars take precedence

	// AutomaticEnv alone doesn't surface env vars to Unmarshal: viper only
	// unmarshals keys it already knows (defaults or config file). In
	// production there is no .env file, so bind every struct key explicitly.
	for _, key := range configKeys() {
		_ = v.BindEnv(key)
	}
	// Render env uses the shorter name; accept it as a fallback.
	_ = v.BindEnv("ACCESS_TOKEN_TTL_MINUTES", "ACCESS_TOKEN_TTL_MINUTES", "ACCESS_TOKEN_TTL_MIN")

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal failed: %w", err)
	}

	weights, err := loadWeightTable(cfg.ScoringWeightsPath)
	if err != nil {
		return nil, fmt.Errorf("config: scoring weights: %w", err)
	}
	cfg.WeightTable = weights

	if err := readiness.ValidateWeightTable(weights); err != nil {
		return nil, fmt.Errorf("config: weight validation failed: %w", err)
	}

	return cfg, nil
}

// configKeys returns the mapstructure tag of every Config field.
func configKeys() []string {
	t := reflect.TypeOf(Config{})
	keys := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		if tag := t.Field(i).Tag.Get("mapstructure"); tag != "" {
			keys = append(keys, tag)
		}
	}
	return keys
}

func (c *Config) IsProduction() bool {
	return strings.EqualFold(c.AppEnv, "production")
}

// scoringWeightsFile mirrors the YAML structure.
type scoringWeightsFile struct {
	EngineVersion string                     `yaml:"engine_version"`
	TierWeights   map[int]tierWeightsYAML    `yaml:"tier_weights"`
}

type tierWeightsYAML struct {
	SkillMatch      float64 `yaml:"skill_match"`
	DSASignal       float64 `yaml:"dsa_signal"`
	SystemDesign    float64 `yaml:"system_design"`
	ArchDepth       float64 `yaml:"arch_depth"`
	DomainRelevance float64 `yaml:"domain_relevance"`
	ExperienceMatch float64 `yaml:"experience_match"`
}

func loadWeightTable(path string) (map[candidate.ExperienceTier]readiness.TierWeights, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		// Fall back to defaults if file not found
		return readiness.DefaultWeightTable, nil
	}

	var f scoringWeightsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	table := make(map[candidate.ExperienceTier]readiness.TierWeights, len(f.TierWeights))
	for tierInt, w := range f.TierWeights {
		table[candidate.ExperienceTier(tierInt)] = readiness.TierWeights{
			SkillMatch:      w.SkillMatch,
			DSASignal:       w.DSASignal,
			SystemDesign:    w.SystemDesign,
			ArchDepth:       w.ArchDepth,
			DomainRelevance: w.DomainRelevance,
			ExperienceMatch: w.ExperienceMatch,
		}
	}
	return table, nil
}
