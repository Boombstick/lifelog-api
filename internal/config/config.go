package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	BindHost           string        `mapstructure:"BIND_HOST"`
	ServerPort         string        `mapstructure:"SERVER_PORT"`
	PostgresHost       string        `mapstructure:"POSTGRES_HOST"`
	PostgresPort       string        `mapstructure:"POSTGRES_PORT"`
	PostgresUser       string        `mapstructure:"POSTGRES_USER"`
	PostgresPassword   string        `mapstructure:"POSTGRES_PASSWORD"`
	PostgresDatabase   string        `mapstructure:"POSTGRES_DATABASE"`
	TMDBToken          string        `mapstructure:"TMDB_TOKEN"`
	TMDBBaseUrl        string        `mapstructure:"TMDB_BASE_URL"`
	TMDBPosterPath     string        `mapstructure:"TMDB_POSTER_PATH"`
	TMDBRateLimit      string        `mapstructure:"TMDB_RATE_LIMIT"`
	TMDBTimeout        time.Duration `mapstructure:"TMDB_TIMEOUT"`
	ElasticURL         string        `mapstructure:"ELASTIC_URL"`
	ElasticUsername    string        `mapstructure:"ELASTIC_USERNAME"`
	ElasticPassword    string        `mapstructure:"ELASTIC_PASSWORD"`
	ElasticPrefix      string        `mapstructure:"ELASTIC_INDEX_PREFIX"`
	ElasticReadTimeout time.Duration `mapstructure:"ELASTIC_READ_TIMEOUT"`

	AuthPort                   string        `mapstructure:"AUTH_PORT"`
	CalendarPort               string        `mapstructure:"CALENDAR_PORT"`
	CalendarDatabase           string        `mapstructure:"CALENDAR_POSTGRES_DATABASE"`
	CalendarFilmsURL           string        `mapstructure:"CALENDAR_FILMS_URL"`
	CalendarAuthURL            string        `mapstructure:"CALENDAR_AUTH_URL"`
	CalendarTick               time.Duration `mapstructure:"CALENDAR_TICK"`
	TelegramBotToken           string        `mapstructure:"TELEGRAM_BOT_TOKEN"`
	TelegramAPIBaseURL         string        `mapstructure:"TELEGRAM_API_BASE_URL"`
	TelegramRelaySecret        string        `mapstructure:"TELEGRAM_RELAY_SECRET"`
	CalendarGoogleClientID     string        `mapstructure:"CALENDAR_GOOGLE_CLIENT_ID"`
	CalendarGoogleClientSecret string        `mapstructure:"CALENDAR_GOOGLE_CLIENT_SECRET"`
	CalendarGoogleRedirectURL  string        `mapstructure:"CALENDAR_GOOGLE_REDIRECT_URL"`
	CalendarGoogleTokenKey     string        `mapstructure:"CALENDAR_GOOGLE_TOKEN_KEY"`
	CalendarGoogleSyncEvery    time.Duration `mapstructure:"CALENDAR_GOOGLE_SYNC_EVERY"`
	NotesPort                  string        `mapstructure:"NOTES_PORT"`
	NotesDatabase              string        `mapstructure:"NOTES_POSTGRES_DATABASE"`
	NotifyPort                 string        `mapstructure:"NOTIFY_PORT"`
	NotifyDatabase             string        `mapstructure:"NOTIFY_POSTGRES_DATABASE"`
	NotifyFCMCredentialsFile   string        `mapstructure:"NOTIFY_FCM_CREDENTIALS_FILE"`
	NotifyFCMEndpoint          string        `mapstructure:"NOTIFY_FCM_ENDPOINT"`
	NotifyVAPIDPublicKey       string        `mapstructure:"NOTIFY_VAPID_PUBLIC_KEY"`
	NotifyVAPIDPrivateKey      string        `mapstructure:"NOTIFY_VAPID_PRIVATE_KEY"`
	NotifyVAPIDSubject         string        `mapstructure:"NOTIFY_VAPID_SUBJECT"`
	NotifyAllowTestEndpoints   bool          `mapstructure:"NOTIFY_ALLOW_TEST_ENDPOINTS"`
	NotifyURL                  string        `mapstructure:"NOTIFY_URL"`
	AuthDatabase               string        `mapstructure:"AUTH_POSTGRES_DATABASE"`
	AuthPrivateKeyPath         string        `mapstructure:"AUTH_PRIVATE_KEY_PATH"`
	AuthIssuer                 string        `mapstructure:"AUTH_ISSUER"`
	AuthAudience               string        `mapstructure:"AUTH_AUDIENCE"`
	AuthAccessTTL              time.Duration `mapstructure:"AUTH_ACCESS_TTL"`
	AuthRefreshTTL             time.Duration `mapstructure:"AUTH_REFRESH_TTL"`
	AuthInternalToken          string        `mapstructure:"AUTH_INTERNAL_TOKEN"`
	AuthBotUsername            string        `mapstructure:"AUTH_BOT_USERNAME"`
	AuthMergeTargets           string        `mapstructure:"AUTH_MERGE_TARGETS"`
	AuthJWKSURL                string        `mapstructure:"AUTH_JWKS_URL"`
	AuthJWKSCacheTTL           time.Duration `mapstructure:"AUTH_JWKS_CACHE_TTL"`
	FilmsAuthURL               string        `mapstructure:"FILMS_AUTH_URL"`
	FilmsFollowsTTL            time.Duration `mapstructure:"FILMS_FOLLOWS_TTL"`
	FilmsSearchCacheTTL        time.Duration `mapstructure:"FILMS_SEARCH_CACHE_TTL"`
	AuthFilmsURL               string        `mapstructure:"AUTH_FILMS_URL"`
	AuthRotationDir            string        `mapstructure:"AUTH_ROTATION_DIR"`
	AuthRootAdminLogin         string        `mapstructure:"AUTH_ROOT_ADMIN_LOGIN"`
	AuthRootAdminPassword      string        `mapstructure:"AUTH_ROOT_ADMIN_PASSWORD"`
	AuthRootAdminEmail         string        `mapstructure:"AUTH_ROOT_ADMIN_EMAIL"`
	AuthRootAdminName          string        `mapstructure:"AUTH_ROOT_ADMIN_NAME"`
	AuthSMTPHost               string        `mapstructure:"AUTH_SMTP_HOST"`
	AuthSMTPPort               string        `mapstructure:"AUTH_SMTP_PORT"`
	AuthSMTPUser               string        `mapstructure:"AUTH_SMTP_USER"`
	AuthSMTPPassword           string        `mapstructure:"AUTH_SMTP_PASSWORD"`
	AuthMailFrom               string        `mapstructure:"AUTH_MAIL_FROM"`
	AuthMailCapture            bool          `mapstructure:"AUTH_MAIL_CAPTURE"`
	PublicURL                  string        `mapstructure:"PUBLIC_URL"`

	TripsPort            string        `mapstructure:"TRIPS_PORT"`
	TripsDatabase        string        `mapstructure:"TRIPS_POSTGRES_DATABASE"`
	TripsPhotoPath       string        `mapstructure:"TRIPS_PHOTO_PATH"`
	TripsCategoriesIndex string        `mapstructure:"TRIPS_CATEGORIES_INDEX"`
	TripsRatesBaseURL    string        `mapstructure:"TRIPS_RATES_BASE_URL"`
	TripsRatesAPIKey     string        `mapstructure:"TRIPS_RATES_API_KEY"`
	TripsRatesAPIVersion string        `mapstructure:"TRIPS_RATES_API_VERSION"`
	TripsRatesEvery      time.Duration `mapstructure:"TRIPS_RATES_EVERY"`

	VisitsEnabled bool   `mapstructure:"VISITS_ENABLED"`
	VisitsAuthURL string `mapstructure:"VISITS_AUTH_URL"`
}

func (c *Config) TMDBRateLimitRPS() float64 {
	parsed, err := strconv.ParseFloat(c.TMDBRateLimit, 64)
	if err != nil || parsed <= 0 {
		return 5
	}
	return parsed
}

func New() (*Config, error) {
	viper.SetDefault("BIND_HOST", "")
	viper.SetDefault("SERVER_PORT", "9091")
	viper.SetDefault("POSTGRES_HOST", "localhost")
	viper.SetDefault("POSTGRES_PORT", "5432")
	viper.SetDefault("POSTGRES_USER", "postgres")
	viper.SetDefault("POSTGRES_PASSWORD", "postgres")
	viper.SetDefault("POSTGRES_DATABASE", "postgres")

	viper.SetDefault("TMDB_TOKEN", "")
	viper.SetDefault("TMDB_BASE_URL", "https://api.themoviedb.org/3")
	viper.SetDefault("TMDB_POSTER_PATH", "https://image.tmdb.org/t/p")
	viper.SetDefault("TMDB_RATE_LIMIT", "5")
	viper.SetDefault("TMDB_TIMEOUT", "10s")

	viper.SetDefault("ELASTIC_URL", "http://localhost:9200")
	viper.SetDefault("ELASTIC_USERNAME", "")
	viper.SetDefault("ELASTIC_PASSWORD", "")
	viper.SetDefault("ELASTIC_INDEX_PREFIX", "lifelog")
	viper.SetDefault("ELASTIC_READ_TIMEOUT", "5s")

	viper.SetDefault("AUTH_PORT", "9092")
	viper.SetDefault("CALENDAR_PORT", "9093")
	viper.SetDefault("CALENDAR_POSTGRES_DATABASE", "lifelog_calendar")
	viper.SetDefault("CALENDAR_FILMS_URL", "http://films:9091/api/v1")
	viper.SetDefault("CALENDAR_AUTH_URL", "http://auth:9092")

	viper.SetDefault("CALENDAR_TICK", "1m")
	viper.SetDefault("TELEGRAM_BOT_TOKEN", "")

	viper.SetDefault("TELEGRAM_API_BASE_URL", "")
	viper.SetDefault("TELEGRAM_RELAY_SECRET", "")
	viper.SetDefault("CALENDAR_GOOGLE_CLIENT_ID", "")
	viper.SetDefault("CALENDAR_GOOGLE_CLIENT_SECRET", "")
	viper.SetDefault("CALENDAR_GOOGLE_REDIRECT_URL", "")
	viper.SetDefault("CALENDAR_GOOGLE_TOKEN_KEY", "")
	viper.SetDefault("CALENDAR_GOOGLE_SYNC_EVERY", "30m")
	viper.SetDefault("NOTES_PORT", "9094")
	viper.SetDefault("NOTES_POSTGRES_DATABASE", "lifelog_notes")
	viper.SetDefault("NOTIFY_PORT", "9095")
	viper.SetDefault("NOTIFY_POSTGRES_DATABASE", "lifelog_notify")

	viper.SetDefault("NOTIFY_FCM_CREDENTIALS_FILE", "")
	viper.SetDefault("NOTIFY_FCM_ENDPOINT", "https://fcm.googleapis.com")

	viper.SetDefault("NOTIFY_VAPID_PUBLIC_KEY", "")
	viper.SetDefault("NOTIFY_VAPID_PRIVATE_KEY", "")
	viper.SetDefault("NOTIFY_VAPID_SUBJECT", "mailto:noreply@lifel0g.ru")

	viper.SetDefault("NOTIFY_ALLOW_TEST_ENDPOINTS", false)

	viper.SetDefault("NOTIFY_URL", "")
	viper.SetDefault("AUTH_POSTGRES_DATABASE", "lifelog_auth")
	viper.SetDefault("AUTH_PRIVATE_KEY_PATH", "./secrets/auth_signing_key.pem")
	viper.SetDefault("AUTH_ISSUER", "lifelog-auth")
	viper.SetDefault("AUTH_AUDIENCE", "lifelog")

	viper.SetDefault("AUTH_ACCESS_TTL", "15m")
	viper.SetDefault("AUTH_REFRESH_TTL", "720h")
	viper.SetDefault("AUTH_INTERNAL_TOKEN", "")
	viper.SetDefault("AUTH_BOT_USERNAME", "")
	viper.SetDefault("AUTH_MERGE_TARGETS", "")
	viper.SetDefault("AUTH_JWKS_URL", "")
	viper.SetDefault("AUTH_JWKS_CACHE_TTL", "1h")

	viper.SetDefault("FILMS_AUTH_URL", "http://auth:9092")
	viper.SetDefault("FILMS_FOLLOWS_TTL", "1m")
	viper.SetDefault("FILMS_SEARCH_CACHE_TTL", "30s")
	viper.SetDefault("AUTH_FILMS_URL", "http://films:9091/api/v1")
	viper.SetDefault("AUTH_ROTATION_DIR", "")
	viper.SetDefault("AUTH_ROOT_ADMIN_LOGIN", "admin")
	viper.SetDefault("AUTH_ROOT_ADMIN_PASSWORD", "")
	viper.SetDefault("AUTH_ROOT_ADMIN_EMAIL", "")
	viper.SetDefault("AUTH_ROOT_ADMIN_NAME", "")
	viper.SetDefault("AUTH_SMTP_HOST", "")
	viper.SetDefault("AUTH_SMTP_PORT", "465")
	viper.SetDefault("AUTH_SMTP_USER", "")
	viper.SetDefault("AUTH_SMTP_PASSWORD", "")
	viper.SetDefault("AUTH_MAIL_FROM", "")
	viper.SetDefault("AUTH_MAIL_CAPTURE", false)
	viper.SetDefault("PUBLIC_URL", "https://lifel0g.ru")

	viper.SetDefault("TRIPS_PORT", "8080")
	viper.SetDefault("TRIPS_POSTGRES_DATABASE", "lifelog")
	viper.SetDefault("TRIPS_PHOTO_PATH", "/app/uploads/trip-photos")
	viper.SetDefault("TRIPS_CATEGORIES_INDEX", "categories")
	viper.SetDefault("TRIPS_RATES_BASE_URL", "https://v6.exchangerate-api.com")
	viper.SetDefault("TRIPS_RATES_API_KEY", "")
	viper.SetDefault("TRIPS_RATES_API_VERSION", "6")
	viper.SetDefault("TRIPS_RATES_EVERY", "12h")
	viper.SetDefault("VISITS_ENABLED", false)
	viper.SetDefault("VISITS_AUTH_URL", "http://localhost:9092")

	viper.SetConfigFile("config.cfg")
	viper.SetConfigType("env")
	if err := viper.ReadInConfig(); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				return nil, fmt.Errorf("ошибка чтения config.cfg: %w", err)
			}
		}
	}
	viper.AutomaticEnv()

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("ошибка парсинга конфига: %w", err)
	}
	if err := cfg.rejectTestFlags(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) rejectTestFlags() error {
	if os.Getenv("GIN_MODE") != "release" {
		return nil
	}
	if c.AuthMailCapture {
		return errors.New("AUTH_MAIL_CAPTURE must be off with GIN_MODE=release: it exposes verification codes")
	}
	if c.NotifyAllowTestEndpoints {
		return errors.New("NOTIFY_ALLOW_TEST_ENDPOINTS must be off with GIN_MODE=release: it lets push reach internal hosts")
	}
	return nil
}
