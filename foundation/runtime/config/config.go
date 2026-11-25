package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Address, DatabaseURL string
	SecureCookies        bool
	Governance           governance.Options
	OIDCProviders        []identity.OIDCProviderConfig
	SAMLProviders        []identity.SAMLProviderConfig
	PlatformOperatorIDs  []string
	AuditPreviousKeys    []ed25519.PublicKey
	AuditOnlineDays      int
	AuditRetentionDays   int
	SMSURL, SMSToken     string
}

func Load() (Config, error) {
	c := Config{Address: os.Getenv("API_ADDR"), DatabaseURL: os.Getenv("DATABASE_URL")}
	if c.Address == "" {
		c.Address = "127.0.0.1:8080"
	}
	if raw := os.Getenv("COOKIE_SECURE"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return c, errors.New("COOKIE_SECURE must be boolean")
		}
		c.SecureCookies = parsed
	}
	host, _, err := net.SplitHostPort(c.Address)
	if err != nil {
		return c, err
	}
	ip := net.ParseIP(host)
	if (ip == nil || !ip.IsLoopback()) && !c.SecureCookies {
		return c, errors.New("non-loopback deployments require COOKIE_SECURE=true and HTTPS")
	}

	c.Governance.StorageRoot = os.Getenv("FILE_STORAGE_ROOT")
	if c.Governance.StorageRoot == "" {
		if ip == nil || !ip.IsLoopback() {
			return c, errors.New("FILE_STORAGE_ROOT required")
		}
		storagePath := ".local/storage"
		// Existing checkouts may still have ignored storage under services/.
		for _, candidate := range []string{
			filepath.Join("api", storagePath),
			storagePath,
			filepath.Join("..", "services", "api", storagePath),
			filepath.Join("..", "services", storagePath),
		} {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				storagePath = candidate
				break
			} else if err != nil && !os.IsNotExist(err) {
				return c, err
			}
		}
		root, e := filepath.Abs(storagePath)
		if e != nil {
			return c, e
		}
		c.Governance.StorageRoot = root
	}
	if !filepath.IsAbs(c.Governance.StorageRoot) {
		return c, errors.New("FILE_STORAGE_ROOT must be absolute")
	}
	if raw := os.Getenv("SETTINGS_ENCRYPTION_KEY"); raw != "" {
		key, e := base64.StdEncoding.DecodeString(raw)
		if e != nil || len(key) != 32 {
			return c, errors.New("SETTINGS_ENCRYPTION_KEY must be base64 encoded 32 bytes")
		}
		c.Governance.EncryptionKey = key
	} else if ip == nil || !ip.IsLoopback() {
		return c, errors.New("SETTINGS_ENCRYPTION_KEY required")
	}
	if raw := os.Getenv("IAM_AUDIT_SIGNING_SEED"); raw != "" {
		seed, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(seed) != ed25519.SeedSize {
			return c, errors.New("IAM_AUDIT_SIGNING_SEED must be a base64 encoded Ed25519 seed")
		}
		c.Governance.AuditSigningSeed = seed
	}
	if raw := os.Getenv("IAM_AUDIT_PREVIOUS_PUBLIC_KEYS"); raw != "" {
		values := strings.Split(raw, ",")
		if len(values) > 32 {
			return c, errors.New("IAM_AUDIT_PREVIOUS_PUBLIC_KEYS supports at most 32 keys")
		}
		for _, key := range values {
			decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(key))
			if err != nil || len(decoded) != ed25519.PublicKeySize {
				return c, errors.New("IAM_AUDIT_PREVIOUS_PUBLIC_KEYS must contain base64 encoded Ed25519 public keys")
			}
			c.AuditPreviousKeys = append(c.AuditPreviousKeys, ed25519.PublicKey(decoded))
		}
	}
	if raw := os.Getenv("IAM_AUDIT_ONLINE_DAYS"); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil || days < 0 || days > 3650 || (days > 0 && len(c.Governance.AuditSigningSeed) == 0) {
			return c, errors.New("IAM_AUDIT_ONLINE_DAYS must be 0 to 3650 and archiving requires IAM_AUDIT_SIGNING_SEED")
		}
		c.AuditOnlineDays = days
	}
	if raw := os.Getenv("IAM_AUDIT_RETENTION_DAYS"); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil || days < 0 || days > 36500 || (days > 0 && (c.AuditOnlineDays == 0 || days < c.AuditOnlineDays)) {
			return c, errors.New("IAM_AUDIT_RETENTION_DAYS must be 0 to 36500 and expiry requires a positive IAM_AUDIT_ONLINE_DAYS at or below retention")
		}
		c.AuditRetentionDays = days
	}
	c.Governance.KnowledgeRedisURL = os.Getenv("REDIS_URL")
	c.Governance.PublicWorkspace = os.Getenv("PUBLIC_TENANT_ID")
	if c.Governance.PublicWorkspace == "" {
		c.Governance.PublicWorkspace = os.Getenv("PUBLIC_WORKSPACE_ID")
	}
	c.Governance.PublicURL = os.Getenv("PUBLIC_APP_URL")
	c.SMSURL, c.SMSToken = os.Getenv("IAM_SMS_URL"), os.Getenv("IAM_SMS_TOKEN")
	if c.SMSURL != "" || c.SMSToken != "" {
		endpoint, parseErr := url.Parse(c.SMSURL)
		if parseErr != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil ||
			endpoint.RawQuery != "" || endpoint.Fragment != "" || strings.TrimSpace(c.SMSToken) == "" || strings.ContainsAny(c.SMSToken, "\r\n") {
			return c, errors.New("IAM_SMS_URL requires an HTTPS endpoint and IAM_SMS_TOKEN")
		}
	}
	c.PlatformOperatorIDs, err = identity.ParsePlatformOperators(os.Getenv("IAM_PLATFORM_OPERATOR_IDS"))
	if err != nil {
		return c, err
	}
	if raw := os.Getenv("IAM_OIDC_PROVIDERS_JSON"); raw != "" {
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.DisallowUnknownFields()
		if len(raw) > 65536 || decoder.Decode(&c.OIDCProviders) != nil || c.OIDCProviders == nil || decoder.Decode(new(any)) != io.EOF {
			return c, errors.New("IAM_OIDC_PROVIDERS_JSON must be a valid provider array")
		}
		if err := identity.ValidateOIDCProviders(c.OIDCProviders, c.Governance.PublicURL, c.SecureCookies); err != nil {
			return c, err
		}
		if len(c.OIDCProviders) > 0 && len(c.Governance.EncryptionKey) != 32 {
			return c, errors.New("OIDC requires SETTINGS_ENCRYPTION_KEY")
		}
	}
	if raw := os.Getenv("IAM_SAML_PROVIDERS_JSON"); raw != "" {
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.DisallowUnknownFields()
		if len(raw) > 262144 || decoder.Decode(&c.SAMLProviders) != nil || c.SAMLProviders == nil || decoder.Decode(new(any)) != io.EOF {
			return c, errors.New("IAM_SAML_PROVIDERS_JSON must be a valid provider array")
		}
		if err := identity.ValidateSAMLProviders(c.SAMLProviders, c.Governance.PublicURL, c.SecureCookies); err != nil {
			return c, err
		}
		for _, saml := range c.SAMLProviders {
			for _, oidc := range c.OIDCProviders {
				if saml.ID == oidc.ID {
					return c, errors.New("external identity provider IDs must be unique across protocols")
				}
			}
		}
		if len(c.SAMLProviders) > 0 && len(c.Governance.EncryptionKey) != 32 {
			return c, errors.New("SAML requires SETTINGS_ENCRYPTION_KEY")
		}
	}
	if c.SecureCookies {
		origin, e := url.Parse(c.Governance.PublicURL)
		if e != nil || origin.Scheme != "https" || origin.Hostname() == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") {
			return c, errors.New("secure deployments require PUBLIC_APP_URL as an HTTPS origin")
		}
	}
	for _, host := range strings.Split(os.Getenv("SMTP_ALLOWED_HOSTS"), ",") {
		if h := strings.TrimSpace(host); h != "" {
			c.Governance.SMTPHosts = append(c.Governance.SMTPHosts, h)
		}
	}
	// MAIL_* business limits are mail plugin options resolved by the
	// deployment; the platform config no longer carries them.
	return c, nil
}
