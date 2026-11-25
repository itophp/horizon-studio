package identity

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"
	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/audit"
	"horizon/internal/repository/dbgen"
)

type OIDCProviderConfig struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Issuer       string `json:"issuer"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	RedirectURL  string `json:"redirectUrl"`
}

type oidcProvider struct {
	config   OIDCProviderConfig
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	client   *http.Client
}

var providerIDPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,39}$`)

func ValidateOIDCProviders(configs []OIDCProviderConfig, publicURL string, secure bool) error {
	if len(configs) > 20 {
		return errors.New("too many OIDC providers")
	}
	seen := make(map[string]bool)
	for _, config := range configs {
		if !providerIDPattern.MatchString(config.ID) || seen[config.ID] || len(config.Name) == 0 || len(config.Name) > 100 ||
			len(config.ClientID) == 0 || len(config.ClientID) > 500 || len(config.ClientSecret) > 4096 {
			return errors.New("invalid OIDC provider configuration")
		}
		seen[config.ID] = true
		issuer, err := url.Parse(config.Issuer)
		if err != nil || !oidcURLAllowed(issuer, secure) || issuer.RawQuery != "" {
			return errors.New("OIDC issuer must use HTTPS or an explicit local loopback address")
		}
		redirect, err := url.Parse(config.RedirectURL)
		origin, originErr := url.Parse(publicURL)
		if err != nil || originErr != nil || !oidcURLAllowed(redirect, secure) || origin.Scheme != redirect.Scheme || origin.Host != redirect.Host ||
			redirect.Path != "/login/oidc" || redirect.RawQuery != "" {
			return errors.New("OIDC redirect must be the public origin's /login/oidc route")
		}
	}
	return nil
}

func oidcURLAllowed(value *url.URL, secure bool) bool {
	if value == nil || value.Host == "" || value.User != nil || value.Fragment != "" || value.Opaque != "" {
		return false
	}
	return value.Scheme == "https" || !secure && value.Scheme == "http" && net.ParseIP(value.Hostname()).IsLoopback()
}

func (s *Service) ConfigureOIDC(ctx context.Context, configs []OIDCProviderConfig, publicURL string) error {
	if err := ValidateOIDCProviders(configs, publicURL, s.SecureCookies); err != nil {
		return err
	}
	providers := make(map[string]*oidcProvider, len(configs))
	for _, config := range configs {
		if s.samlProviders[config.ID] != nil {
			return errors.New("external identity provider IDs must be unique across protocols")
		}
	}
	for _, config := range configs {
		client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) > 3 || !oidcURLAllowed(request.URL, s.SecureCookies) {
				return errors.New("OIDC redirect rejected")
			}
			return nil
		}}
		provider, err := oidc.NewProvider(oidc.ClientContext(ctx, client), config.Issuer)
		if err != nil {
			return errors.New("OIDC provider discovery failed")
		}
		endpoint := provider.Endpoint()
		var discovery struct {
			JWKSURL string `json:"jwks_uri"`
		}
		if provider.Claims(&discovery) != nil {
			return errors.New("OIDC provider discovery invalid")
		}
		for _, raw := range []string{endpoint.AuthURL, endpoint.TokenURL, discovery.JWKSURL} {
			parsed, err := url.Parse(raw)
			if err != nil || !oidcURLAllowed(parsed, s.SecureCookies) {
				return errors.New("OIDC discovery contains an insecure endpoint")
			}
		}
		providers[config.ID] = &oidcProvider{config: config, client: client,
			oauth: oauth2.Config{ClientID: config.ClientID, ClientSecret: config.ClientSecret, RedirectURL: config.RedirectURL,
				Endpoint: endpoint, Scopes: []string{oidc.ScopeOpenID}},
			verifier: provider.Verifier(&oidc.Config{ClientID: config.ClientID, SupportedSigningAlgs: []string{oidc.RS256, oidc.ES256, oidc.EdDSA}}),
		}
	}
	s.oidcProviders = providers
	return nil
}

func (s *Service) FederationProviders(context.Context) ([]*genidentity.FederationProvider, error) {
	providers := make([]*genidentity.FederationProvider, 0, len(s.oidcProviders))
	for id, provider := range s.oidcProviders {
		providers = append(providers, &genidentity.FederationProvider{ID: id, Name: provider.config.Name})
	}
	for id, provider := range s.samlProviders {
		providers = append(providers, &genidentity.FederationProvider{ID: id, Name: provider.config.Name})
	}
	slices.SortFunc(providers, func(left, right *genidentity.FederationProvider) int { return strings.Compare(left.ID, right.ID) })
	return providers, nil
}

func federationCookieName(state string) string { return "horizon_oidc_" + tokenHash(state)[:16] }

func (s *Service) StartFederation(ctx context.Context, payload *genidentity.StartFederationPayload) (*genidentity.FederationStartResult, error) {
	if provider := s.samlProviders[payload.ProviderID]; provider != nil {
		return s.startSAML(ctx, provider, payload.Purpose)
	}
	provider := s.oidcProviders[payload.ProviderID]
	if provider == nil {
		return nil, problem("invalid")
	}
	if s.repo == nil || s.mfaSecrets == nil {
		return nil, problem("unavailable")
	}
	state, err := randomToken()
	if err != nil {
		return nil, safe(err)
	}
	browser, err := randomToken()
	if err != nil {
		return nil, safe(err)
	}
	nonce, err := randomToken()
	if err != nil {
		return nil, safe(err)
	}
	verifier := oauth2.GenerateVerifier()
	sealed, err := s.mfaSecrets.Seal("iam", "oidc", state, verifier)
	if err != nil {
		return nil, problem("unavailable")
	}
	if err = s.repo.q.IAMStartFederation(ctx, dbgen.IAMStartFederationParams{
		StateHash: tokenHash(state), BrowserHash: tokenHash(browser), ProviderID: payload.ProviderID, Issuer: provider.config.Issuer,
		Purpose: payload.Purpose, NonceHash: tokenHash(nonce), SealedVerifier: sealed,
	}); err != nil {
		return nil, safe(err)
	}
	cookie := (&http.Cookie{Name: federationCookieName(state), Value: browser, Path: "/api/auth/federation", MaxAge: 300,
		HttpOnly: true, Secure: s.SecureCookies, SameSite: http.SameSiteLaxMode}).String()
	return &genidentity.FederationStartResult{AuthorizationURL: provider.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), Cookie: cookie}, nil
}

func (s *Service) CompleteFederation(ctx context.Context, payload *genidentity.CompleteFederationPayload) (*genidentity.FederationCompleteResult, error) {
	if s.repo == nil || s.mfaSecrets == nil {
		return nil, problem("unavailable")
	}
	request := &http.Request{Header: http.Header{"Cookie": []string{payload.Cookie}}}
	browser, err := request.Cookie(federationCookieName(payload.State))
	if err != nil || len(browser.Value) != 64 {
		return nil, problem("unauthenticated")
	}
	flow, err := s.repo.q.IAMConsumeFederation(ctx, dbgen.IAMConsumeFederationParams{StateHash: tokenHash(payload.State), BrowserHash: tokenHash(browser.Value)})
	if err != nil {
		return nil, problem("unauthenticated")
	}
	provider := s.oidcProviders[flow.ProviderID]
	if provider == nil || flow.Issuer != provider.config.Issuer {
		return nil, problem("unauthenticated")
	}
	verifier, err := s.mfaSecrets.Unseal("iam", "oidc", payload.State, flow.SealedVerifier)
	if err != nil {
		return nil, problem("unavailable")
	}
	exchangeContext := oidc.ClientContext(ctx, provider.client)
	token, err := provider.oauth.Exchange(exchangeContext, payload.Code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, problem("unauthenticated")
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || len(raw) > 32768 {
		return nil, problem("unauthenticated")
	}
	verified, err := provider.verifier.Verify(exchangeContext, raw)
	if err != nil || len(verified.Subject) == 0 || len(verified.Subject) > 2000 ||
		subtle.ConstantTimeCompare([]byte(tokenHash(verified.Nonce)), []byte(flow.NonceHash)) != 1 {
		return nil, problem("unauthenticated")
	}
	var claims struct {
		AuthorizedParty string `json:"azp"`
	}
	if verified.Claims(&claims) != nil || (len(verified.Audience) > 1 || claims.AuthorizedParty != "") && claims.AuthorizedParty != provider.config.ClientID ||
		verified.AccessTokenHash != "" && verified.VerifyAccessToken(token.AccessToken) != nil {
		return nil, problem("unauthenticated")
	}
	return s.completeExternalIdentity(ctx, flow.ProviderID, flow.Issuer, verified.Subject, flow.Purpose, payload.MfaCode, payload.RecoveryCode)
}

func (s *Service) completeExternalIdentity(ctx context.Context, providerID, issuer, subject, purpose string, mfa, recovery *string) (*genidentity.FederationCompleteResult, error) {
	if purpose == "link" {
		proof, err := randomToken()
		if err != nil {
			return nil, safe(err)
		}
		if err = s.repo.q.IAMStartExternalProof(ctx, dbgen.IAMStartExternalProofParams{
			ProofHash: tokenHash(proof), ProviderID: providerID, Issuer: issuer, Subject: subject,
		}); err != nil {
			return nil, safe(err)
		}
		return &genidentity.FederationCompleteResult{LinkProof: &proof}, nil
	}
	return s.externalLogin(ctx, providerID, issuer, subject, mfa, recovery)
}

func (s *Service) externalLogin(ctx context.Context, provider, issuer, subject string, mfa, recovery *string) (*genidentity.FederationCompleteResult, error) {
	namespace, err := applicationAuthentication(ctx)
	if err != nil {
		return nil, err
	}
	loginID, err := s.repo.q.IAMFindUnscopedExternalLogin(ctx, dbgen.IAMFindUnscopedExternalLoginParams{
		ProviderID: provider, Issuer: issuer, Subject: subject, Realm: namespace,
	})
	if err != nil {
		return nil, problem("unauthenticated")
	}
	policy := DefaultSecurityPolicy()
	throttleKey := tokenHash("external:" + loginID.String())
	throttle, err := s.repo.q.IAMLoginThrottle(ctx, throttleKey)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, safe(err)
	}
	if err == nil && throttle.Failures >= int32(policy.LockAttempts) && throttle.LockedUntil.Time.After(time.Now()) {
		return nil, problem("unauthenticated")
	}
	secret, err := randomToken()
	if err != nil {
		return nil, safe(err)
	}
	csrf, err := randomToken()
	if err != nil {
		return nil, safe(err)
	}
	tx, err := s.repo.pool.Begin(ctx)
	if err != nil {
		return nil, safe(err)
	}
	defer tx.Rollback(ctx)
	queries := dbgen.New(tx)
	if _, err = queries.IAMLockActiveLogin(ctx, loginID); err != nil {
		return nil, problem("unauthenticated")
	}
	valid, err := s.verifySecondFactor(ctx, queries, loginID, mfa, recovery)
	if err != nil {
		return nil, err
	}
	if !valid {
		if err = queries.GovLoginFailure(ctx, dbgen.GovLoginFailureParams{AttemptKey: throttleKey, MaxFailures: int32(policy.LockAttempts), LockMinutes: int32(policy.LockMinutes)}); err != nil {
			return nil, safe(err)
		}
		if err = appendLoginDenial(ctx, queries, loginID, "external_second_factor"); err != nil {
			return nil, safe(err)
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, safe(err)
		}
		return nil, problem("unauthenticated")
	}
	if err = queries.SecurityAuditSetCorrelation(ctx, audit.Correlation(ctx)); err != nil {
		return nil, safe(err)
	}
	device, _ := ctx.Value(deviceLabelKey{}).(string)
	if _, err = queries.IAMCreateUnscopedExternalSession(ctx, dbgen.IAMCreateUnscopedExternalSessionParams{
		LoginID: loginID, SessionHash: tokenHash(secret), CsrfToken: csrf, DeviceLabel: device,
		ProviderID: provider, Issuer: issuer, Subject: subject,
	}); err != nil {
		return nil, problem("unauthenticated")
	}
	if err = queries.IAMLoginClear(ctx, throttleKey); err != nil {
		return nil, safe(err)
	}
	if err = queries.IAMPruneUnscopedLoginSessions(ctx); err != nil {
		return nil, safe(err)
	}
	if err = queries.SecurityAuditAppend(ctx, dbgen.SecurityAuditAppendParams{
		Action: "identity.login", Outcome: "allowed", Reason: "external_credentials_verified",
		CorrelationID: audit.Correlation(ctx), ResourceType: "login_account", ResourceID: loginID.String(),
	}); err != nil {
		return nil, safe(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, safe(err)
	}
	result, err := s.completeInteractiveLogin(ctx, secret, csrf, policy)
	if err != nil {
		return nil, err
	}
	return &genidentity.FederationCompleteResult{Profile: result.Profile, Cookie: &result.Cookie}, nil
}
