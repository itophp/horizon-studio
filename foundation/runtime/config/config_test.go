package config

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"horizon/foundation/system/identity"
)

func TestLocalStorageUsesExistingLegacyDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("API_ADDR", "127.0.0.1:8080")
	t.Setenv("FILE_STORAGE_ROOT", "")
	t.Setenv("COOKIE_SECURE", "false")
	legacy := filepath.Join(dir, "api", ".local", "storage")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Governance.StorageRoot != legacy {
		t.Fatalf("storage root = %q, want %q", c.Governance.StorageRoot, legacy)
	}
}

func TestOIDCConfigurationRejectsUnsafeProviders(t *testing.T) {
	t.Setenv("API_ADDR", "127.0.0.1:8080")
	t.Setenv("COOKIE_SECURE", "false")
	t.Setenv("PUBLIC_APP_URL", "http://127.0.0.1:3000")
	t.Setenv("SETTINGS_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	valid := identity.OIDCProviderConfig{ID: "fixture", Name: "Test", Issuer: "https://idp.example.test", ClientID: "test-client", RedirectURL: "http://127.0.0.1:3000/login/oidc"}
	encode := func(providers ...identity.OIDCProviderConfig) string {
		encoded, err := json.Marshal(providers)
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}
	t.Setenv("IAM_OIDC_PROVIDERS_JSON", encode(valid))
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*identity.OIDCProviderConfig){
		"plain HTTP remote":       func(p *identity.OIDCProviderConfig) { p.Issuer = "http://idp.example.test" },
		"localhost alias":         func(p *identity.OIDCProviderConfig) { p.Issuer = "http://localhost:8081" },
		"issuer userinfo":         func(p *identity.OIDCProviderConfig) { p.Issuer = "https://secret@idp.example.test" },
		"redirect foreign origin": func(p *identity.OIDCProviderConfig) { p.RedirectURL = "https://other.test/login/oidc" },
		"redirect wrong path":     func(p *identity.OIDCProviderConfig) { p.RedirectURL = "http://127.0.0.1:3000/login" },
		"redirect query":          func(p *identity.OIDCProviderConfig) { p.RedirectURL += "?token=secret" },
		"provider ID":             func(p *identity.OIDCProviderConfig) { p.ID = "../fixture" },
	} {
		t.Run(name, func(t *testing.T) {
			provider := valid
			mutate(&provider)
			t.Setenv("IAM_OIDC_PROVIDERS_JSON", encode(provider))
			if _, err := Load(); err == nil {
				t.Fatal("unsafe provider accepted")
			}
		})
	}
	for _, raw := range []string{"null", encode(valid, valid), `{"id":"fixture"}`, `[{"id":"fixture","unknown":true}]`, encode(valid) + " {}"} {
		t.Setenv("IAM_OIDC_PROVIDERS_JSON", raw)
		if _, err := Load(); err == nil {
			t.Fatal("invalid provider configuration accepted")
		}
	}
	t.Setenv("IAM_OIDC_PROVIDERS_JSON", encode(valid))
	t.Setenv("SETTINGS_ENCRYPTION_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("OIDC accepted without a verifier encryption key")
	}
}

func TestSAMLConfigurationRejectsMalformedDeploymentInput(t *testing.T) {
	t.Setenv("API_ADDR", "127.0.0.1:8080")
	t.Setenv("COOKIE_SECURE", "true")
	t.Setenv("PUBLIC_APP_URL", "https://console.test")
	t.Setenv("SETTINGS_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("IAM_OIDC_PROVIDERS_JSON", "[]")
	t.Setenv("IAM_SAML_PROVIDERS_JSON", "[]")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"null", "{}", "[] {}", `[{"unknown":true}]`, strings.Repeat(" ", 262145), `[{"id":"saml","name":"SAML","metadataXml":"bad"}]`} {
		t.Setenv("IAM_SAML_PROVIDERS_JSON", raw)
		if _, err := Load(); err == nil {
			t.Fatal("invalid SAML deployment configuration accepted")
		}
	}
}

func TestLocalStorageUsesPreviousServiceDirectory(t *testing.T) {
	repo := t.TempDir()
	foundation := filepath.Join(repo, "foundation")
	legacy := filepath.Join(repo, "services", ".local", "storage")
	for _, dir := range []string{foundation, legacy} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(foundation)
	t.Setenv("API_ADDR", "127.0.0.1:8080")
	t.Setenv("FILE_STORAGE_ROOT", "")
	t.Setenv("COOKIE_SECURE", "false")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Governance.StorageRoot != legacy {
		t.Fatalf("storage root = %q, want %q", c.Governance.StorageRoot, legacy)
	}
}

func TestDevelopmentTokenRequiresLoopback(t *testing.T) {
	t.Setenv("DEMO_AUTH_TOKEN", "test-only")
	t.Setenv("API_ADDR", "0.0.0.0:8080")
	if _, err := Load(); err == nil {
		t.Fatal("development identity exposed publicly")
	}
	t.Setenv("API_ADDR", "127.0.0.1:8080")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}

func TestSecureDeploymentRequiresPublicHTTPSOrigin(t *testing.T) {
	t.Setenv("COOKIE_SECURE", "true")
	t.Setenv("API_ADDR", "127.0.0.1:8080")
	t.Setenv("DEMO_AUTH_TOKEN", "")
	for _, raw := range []string{"", "http://public.test", "https://user:secret@public.test", "https://public.test/path", "https://public.test?token=value"} {
		t.Setenv("PUBLIC_APP_URL", raw)
		if _, err := Load(); err == nil {
			t.Fatalf("invalid public origin accepted: %s", raw)
		}
	}
	t.Setenv("PUBLIC_APP_URL", "https://public.test")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}

// Mail endpoint and limit validation moved to the mail plugin's
// OptionsFromEnv; see applications/mail/options_test.go.
