// Package config parses and validates environment configuration once at
// startup. Invalid configuration is a startup failure.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config is the fully validated runtime configuration.
type Config struct {
	ControlDomain         string
	PreviewDomain         string
	BrandName             string
	Listen                string
	BuildsDir             string
	DBDSN                 string
	DefaultLanguage       string
	TokenTTLSeconds       int
	MaxArchiveBytes       int64
	DiskHighWaterPercent  int
	DiskCriticalPercent   int
	AbuseContact          string
	ACMEDNSProvider       string
	ACMEDNSToken          string
	ReadOnly              bool
}

// Load reads SITEPASS_* environment variables and validates them.
func Load() (Config, error) {
	cfg := Config{
		ControlDomain:   strings.TrimSpace(os.Getenv("SITEPASS_CONTROL_DOMAIN")),
		PreviewDomain:   strings.TrimSpace(os.Getenv("SITEPASS_PREVIEW_DOMAIN")),
		BrandName:       strings.TrimSpace(os.Getenv("SITEPASS_BRAND_NAME")),
		Listen:          strings.TrimSpace(os.Getenv("SITEPASS_LISTEN")),
		BuildsDir:       strings.TrimSpace(os.Getenv("SITEPASS_BUILDS_DIR")),
		DBDSN:           strings.TrimSpace(os.Getenv("SITEPASS_DB_DSN")),
		DefaultLanguage: strings.TrimSpace(os.Getenv("SITEPASS_DEFAULT_LANGUAGE")),
		AbuseContact:    strings.TrimSpace(os.Getenv("SITEPASS_ABUSE_CONTACT")),
		ACMEDNSProvider: strings.TrimSpace(os.Getenv("SITEPASS_ACME_DNS_PROVIDER")),
		ACMEDNSToken:    strings.TrimSpace(os.Getenv("SITEPASS_ACME_DNS_TOKEN")),
		ReadOnly:        parseBoolEnv("SITEPASS_READ_ONLY"),
	}

	var err error
	if cfg.TokenTTLSeconds, err = requiredInt("SITEPASS_TOKEN_TTL_SECONDS"); err != nil {
		return Config{}, err
	}
	if cfg.MaxArchiveBytes, err = requiredInt64("SITEPASS_MAX_ARCHIVE_BYTES"); err != nil {
		return Config{}, err
	}
	if cfg.DiskHighWaterPercent, err = requiredInt("SITEPASS_DISK_HIGH_WATER_PERCENT"); err != nil {
		return Config{}, err
	}
	if cfg.DiskCriticalPercent, err = requiredInt("SITEPASS_DISK_CRITICAL_PERCENT"); err != nil {
		return Config{}, err
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	required := map[string]string{
		"SITEPASS_CONTROL_DOMAIN":   c.ControlDomain,
		"SITEPASS_PREVIEW_DOMAIN":   c.PreviewDomain,
		"SITEPASS_BRAND_NAME":       c.BrandName,
		"SITEPASS_LISTEN":           c.Listen,
		"SITEPASS_BUILDS_DIR":       c.BuildsDir,
		"SITEPASS_DB_DSN":           c.DBDSN,
		"SITEPASS_DEFAULT_LANGUAGE": c.DefaultLanguage,
		"SITEPASS_ABUSE_CONTACT":    c.AbuseContact,
	}
	for name, value := range required {
		if value == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	switch c.DefaultLanguage {
	case "en", "ru", "kk":
	default:
		return fmt.Errorf("SITEPASS_DEFAULT_LANGUAGE must be en, ru, or kk")
	}
	if c.TokenTTLSeconds <= 0 {
		return fmt.Errorf("SITEPASS_TOKEN_TTL_SECONDS must be positive")
	}
	if c.MaxArchiveBytes <= 0 {
		return fmt.Errorf("SITEPASS_MAX_ARCHIVE_BYTES must be positive")
	}
	if c.DiskHighWaterPercent <= 0 || c.DiskHighWaterPercent >= 100 {
		return fmt.Errorf("SITEPASS_DISK_HIGH_WATER_PERCENT must be between 1 and 99")
	}
	if c.DiskCriticalPercent <= c.DiskHighWaterPercent || c.DiskCriticalPercent >= 100 {
		return fmt.Errorf("SITEPASS_DISK_CRITICAL_PERCENT must be greater than high-water and below 100")
	}
	// Shared-apex mode: CONTROL == PREVIEW means previews are served at
	// <label>.<domain> on the same registrable domain as the control site.
	if c.ControlDomain != c.PreviewDomain &&
		registrableSuffix(c.ControlDomain) == registrableSuffix(c.PreviewDomain) {
		return fmt.Errorf("SITEPASS_CONTROL_DOMAIN and SITEPASS_PREVIEW_DOMAIN must be different registrable domains (or set them equal for shared-apex mode)")
	}
	return nil
}

func requiredInt(name string) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s is not a valid integer: %s", name, raw)
	}
	return v, nil
}

func requiredInt64(name string) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s is not a valid integer: %s", name, raw)
	}
	return v, nil
}

func parseBoolEnv(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// registrableSuffix returns a coarse eTLD+1 approximation for bootstrap
// safety checks. It is intentionally simple; bootstrap also verifies DNS.
func registrableSuffix(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return host
	}
	return parts[len(parts)-2] + "." + parts[len(parts)-1]
}
