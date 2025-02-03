package config

import (
	"net/url"
	"reflect"

	"github.com/caarlos0/env/v10"
	"github.com/joho/godotenv"
)

type Config struct {
	// === Core System Configuration ===
	DatabaseURL string `env:"DATABASE_URL" envDefault:"postgres://pentagiuser:pentagipass@pgvector:5432/pentagidb?sslmode=disable"`
	Debug       bool   `env:"DEBUG" envDefault:"false"`
	DataDir     string `env:"DATA_DIR" envDefault:"./data"`
	AskUser     bool   `env:"ASK_USER" envDefault:"false"`

	// === Docker Runtime Configuration ===
	DockerInside       bool   `env:"DOCKER_INSIDE" envDefault:"false"`
	DockerSocket       string `env:"DOCKER_SOCKET"`
	DockerNetwork      string `env:"DOCKER_NETWORK"`
	DockerPublicIP     string `env:"DOCKER_PUBLIC_IP" envDefault:"0.0.0.0"`
	DockerWorkDir      string `env:"DOCKER_WORK_DIR"`
	DockerDefaultImage string `env:"DOCKER_DEFAULT_IMAGE" envDefault:"debian:latest"`

	// === HTTP and GraphQL Server Configuration ===
	ServerPort   int    `env:"SERVER_PORT" envDefault:"8080"`
	ServerHost   string `env:"SERVER_HOST" envDefault:"0.0.0.0"`
	ServerUseSSL bool   `env:"SERVER_USE_SSL" envDefault:"false"`
	ServerSSLKey string `env:"SERVER_SSL_KEY"`
	ServerSSLCrt string `env:"SERVER_SSL_CRT"`

	// === Frontend Static Assets Configuration ===
	StaticURL   *url.URL `env:"STATIC_URL"`
	StaticDir   string   `env:"STATIC_DIR" envDefault:"./fe"`
	CorsOrigins []string `env:"CORS_ORIGINS" envDefault:"*"`

	// === Session Security Configuration ===
	CookieSigningSalt string `env:"COOKIE_SIGNING_SALT"`

	// === Browser Automation Service Configuration ===
	ScraperPublicURL  string `env:"SCRAPER_PUBLIC_URL" envDefault:"https://someuser:somepass@scraper"`
	ScraperPrivateURL string `env:"SCRAPER_PRIVATE_URL" envDefault:"https://someuser:somepass@scraper"`

	// === LLM Provider: OpenAI ===
	OpenAIKey       string `env:"OPEN_AI_KEY"`
	OpenAIServerURL string `env:"OPEN_AI_SERVER_URL" envDefault:"https://api.openai.com/v1"`

	// === LLM Provider: Anthropic ===
	AnthropicAPIKey    string `env:"ANTHROPIC_API_KEY"`
	AnthropicServerURL string `env:"ANTHROPIC_SERVER_URL" envDefault:"https://api.anthropic.com/v1"`

	// === LLM Provider: Custom/Generic ===
	LLMServerURL    string `env:"LLM_SERVER_URL"`
	LLMServerKey    string `env:"LLM_SERVER_KEY"`
	LLMServerModel  string `env:"LLM_SERVER_MODEL"`
	LLMServerConfig string `env:"LLM_SERVER_CONFIG"`

	// === Search Engine: Google Custom Search ===
	GoogleAPIKey string `env:"GOOGLE_API_KEY"`
	GoogleCXKey  string `env:"GOOGLE_CX_KEY"`
	GoogleLRKey  string `env:"GOOGLE_LR_KEY" envDefault:"lang_en"`

	// === OAuth Provider: Google ===
	OAuthGoogleClientID     string `env:"OAUTH_GOOGLE_CLIENT_ID"`
	OAuthGoogleClientSecret string `env:"OAUTH_GOOGLE_CLIENT_SECRET"`

	// === OAuth Provider: GitHub ===
	OAuthGithubClientID     string `env:"OAUTH_GITHUB_CLIENT_ID"`
	OAuthGithubClientSecret string `env:"OAUTH_GITHUB_CLIENT_SECRET"`

	// === Authentication Callback Configuration ===
	PublicURL string `env:"PUBLIC_URL" envDefault:""`

	// === Search Engine: Traversaal ===
	TraversaalAPIKey string `env:"TRAVERSAAL_API_KEY"`

	// === Search Engine: Tavily ===
	TavilyAPIKey string `env:"TAVILY_API_KEY"`

	// === Network Proxy Configuration ===
	ProxyURL string `env:"PROXY_URL"`

	// === OpenTelemetry Configuration ===
	TelemetryEndpoint string `env:"OTEL_HOST"`

	// === Langfuse Observability Configuration ===
	LangfuseBaseURL   string `env:"LANGFUSE_BASE_URL"`
	LangfuseProjectID string `env:"LANGFUSE_PROJECT_ID"`
	LangfusePublicKey string `env:"LANGFUSE_PUBLIC_KEY"`
	LangfuseSecretKey string `env:"LANGFUSE_SECRET_KEY"`
}

func NewConfig() (*Config, error) {
	// Attempt to load .env file (silently ignore if not present)
	_ = godotenv.Load()

	var config Config
	if err := env.ParseWithOptions(&config, env.Options{
		RequiredIfNoDef: false,
		FuncMap: map[reflect.Type]env.ParserFunc{
			reflect.TypeOf(&url.URL{}): func(s string) (interface{}, error) {
				if s == "" {
					return nil, nil
				}
				return url.Parse(s)
			},
		},
	}); err != nil {
		return nil, err
	}

	return &config, nil
}
