package identity

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	dsig "github.com/russellhaering/goxmldsig"

	genidentity "horizon/foundation/gen/identity"
	"horizon/internal/repository/dbgen"
)

type SAMLProviderConfig struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	EntityID       string `json:"entityId"`
	MetadataXML    string `json:"metadataXml"`
	CertificatePEM string `json:"certificatePem"`
	PrivateKeyPEM  string `json:"privateKeyPem"`
}

type samlProvider struct {
	config    SAMLProviderConfig
	sp        saml.ServiceProvider
	returnURL string
}

// Metadata and certificates are deployment trust inputs, never fetched from an
// assertion, URL parameter or remote metadata URL supplied by a browser.
func buildSAMLProviders(configs []SAMLProviderConfig, publicURL string, secure bool) (map[string]*samlProvider, error) {
	if len(configs) > 20 {
		return nil, errors.New("too many SAML providers")
	}
	providers := make(map[string]*samlProvider, len(configs))
	for _, config := range configs {
		if !providerIDPattern.MatchString(config.ID) || providers[config.ID] != nil || len(config.Name) == 0 || len(config.Name) > 100 ||
			len(config.MetadataXML) == 0 || len(config.MetadataXML) > 131072 || len(config.CertificatePEM) > 16384 || len(config.PrivateKeyPEM) > 16384 {
			return nil, errors.New("invalid SAML provider configuration")
		}
		origin, err := url.Parse(publicURL)
		entity, entityErr := url.Parse(config.EntityID)
		if err != nil || !oidcURLAllowed(origin, true) || origin.RawQuery != "" || origin.Path != "" && origin.Path != "/" ||
			entityErr != nil || !oidcURLAllowed(entity, secure) || entity.RawQuery != "" || len(config.EntityID) > 2000 {
			return nil, errors.New("SAML requires a public HTTPS origin and entity identifier")
		}
		metadata, err := samlsp.ParseMetadata([]byte(config.MetadataXML))
		if err != nil || len(metadata.EntityID) == 0 || len(metadata.EntityID) > 2000 || len(metadata.IDPSSODescriptors) == 0 ||
			!metadata.ValidUntil.IsZero() && !metadata.ValidUntil.After(time.Now()) {
			return nil, errors.New("invalid or expired SAML identity provider metadata")
		}
		if err := validateSAMLSigningKeys(metadata, time.Now()); err != nil {
			return nil, err
		}
		pair, err := tls.X509KeyPair([]byte(config.CertificatePEM), []byte(config.PrivateKeyPEM))
		if err != nil {
			return nil, errors.New("invalid SAML service provider certificate/key pair")
		}
		key, ok := pair.PrivateKey.(*rsa.PrivateKey)
		if !ok || key.N.BitLen() < 2048 {
			return nil, errors.New("SAML service provider requires an RSA key of at least 2048 bits")
		}
		certificate, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil || time.Now().Before(certificate.NotBefore) || !time.Now().Before(certificate.NotAfter) {
			return nil, errors.New("SAML service provider certificate is not currently valid")
		}
		acs := origin.ResolveReference(&url.URL{Path: "/api/auth/federation/saml/" + config.ID + "/acs"})
		sp := saml.ServiceProvider{
			EntityID: config.EntityID, Key: key, Certificate: certificate, AcsURL: *acs, MetadataURL: *entity, IDPMetadata: metadata,
			AuthnNameIDFormat: saml.PersistentNameIDFormat, SignatureMethod: dsig.RSASHA256SignatureMethod,
			AssertionConsumerServiceBindings: []string{saml.HTTPPostBinding},
			RejectUnknownConditions:          true, RequireSignedResponseForCBC: true, RejectExpiredIDPMetadata: true,
			RejectSHA1Signatures: true, LimitRelayStateLength: true,
		}
		sso, err := url.Parse(sp.GetSSOBindingLocation(saml.HTTPRedirectBinding))
		if err != nil || !oidcURLAllowed(sso, secure) {
			return nil, errors.New("SAML provider requires a secure HTTP-Redirect SSO endpoint")
		}
		providers[config.ID] = &samlProvider{config: config, sp: sp, returnURL: origin.ResolveReference(&url.URL{Path: "/login/saml"}).String()}
	}
	return providers, nil
}

func validateSAMLSigningKeys(metadata *saml.EntityDescriptor, now time.Time) error {
	valid := false
	for _, descriptor := range metadata.IDPSSODescriptors {
		if descriptor.ValidUntil != nil && !descriptor.ValidUntil.After(now) {
			return errors.New("expired SAML identity provider descriptor")
		}
		for _, key := range descriptor.KeyDescriptors {
			if key.Use != "" && key.Use != "signing" {
				continue
			}
			for _, encoded := range key.KeyInfo.X509Data.X509Certificates {
				der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(encoded.Data), ""))
				if err != nil {
					return errors.New("invalid SAML identity provider signing certificate")
				}
				cert, err := x509.ParseCertificate(der)
				if err != nil {
					return errors.New("invalid SAML identity provider signing certificate")
				}
				switch public := cert.PublicKey.(type) {
				case *rsa.PublicKey:
					if public.N.BitLen() < 2048 {
						return errors.New("weak SAML identity provider signing key")
					}
				case *ecdsa.PublicKey:
					if public.Curve.Params().BitSize < 256 {
						return errors.New("weak SAML identity provider signing key")
					}
				default:
					return errors.New("unsupported SAML identity provider signing key")
				}
				valid = valid || !now.Before(cert.NotBefore) && now.Before(cert.NotAfter)
			}
		}
	}
	if !valid {
		return errors.New("SAML identity provider requires a currently valid signing certificate")
	}
	return nil
}

func ValidateSAMLProviders(configs []SAMLProviderConfig, publicURL string, secure bool) error {
	_, err := buildSAMLProviders(configs, publicURL, secure)
	return err
}

func (s *Service) ConfigureSAML(configs []SAMLProviderConfig, publicURL string) error {
	providers, err := buildSAMLProviders(configs, publicURL, s.SecureCookies)
	if err != nil {
		return err
	}
	for id := range providers {
		if s.oidcProviders[id] != nil {
			return errors.New("external identity provider IDs must be unique across protocols")
		}
	}
	s.samlProviders = providers
	return nil
}

func (s *Service) externalIssuer(providerID string) string {
	if provider := s.oidcProviders[providerID]; provider != nil {
		return provider.config.Issuer
	}
	if provider := s.samlProviders[providerID]; provider != nil {
		return provider.sp.IDPMetadata.EntityID
	}
	return ""
}

func (s *Service) startSAML(ctx context.Context, provider *samlProvider, purpose string) (*genidentity.FederationStartResult, error) {
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
	request, err := provider.sp.MakeAuthenticationRequest(provider.sp.GetSSOBindingLocation(saml.HTTPRedirectBinding), saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		return nil, problem("unavailable")
	}
	redirect, err := request.Redirect(state, &provider.sp)
	if err != nil {
		return nil, problem("unavailable")
	}
	sealed, err := s.mfaSecrets.Seal("iam", "saml", state, request.ID)
	if err != nil {
		return nil, problem("unavailable")
	}
	if err = s.repo.q.IAMStartFederation(ctx, dbgen.IAMStartFederationParams{
		StateHash: tokenHash(state), BrowserHash: tokenHash(browser), ProviderID: provider.config.ID, Issuer: provider.sp.IDPMetadata.EntityID,
		Purpose: purpose, NonceHash: tokenHash(request.ID), SealedVerifier: sealed,
	}); err != nil {
		return nil, safe(err)
	}
	// IdP HTTP-POST is cross-site. Secure SameSite=None is needed only for this
	// one-use flow cookie; the application session remains SameSite=Lax.
	cookie := (&http.Cookie{Name: federationCookieName(state), Value: browser, Path: "/api/auth/federation", MaxAge: 300,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteNoneMode}).String()
	return &genidentity.FederationStartResult{AuthorizationURL: redirect.String(), Cookie: cookie}, nil
}

type samlAssertionCache struct {
	ctx      context.Context
	queries  *dbgen.Queries
	provider string
}

func (c samlAssertionCache) Consume(id string, expiresAt time.Time) error {
	_, err := c.queries.IAMConsumeFederationAssertion(c.ctx, dbgen.IAMConsumeFederationAssertionParams{
		ProviderID: c.provider, AssertionHash: tokenHash(id), ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return saml.ErrAssertionReplayed
	}
	return err
}

const samlCompletionCookie = "horizon_saml_completion"

func (s *Service) SamlCallback(ctx context.Context, payload *genidentity.SamlCallbackPayload) (*genidentity.FederationStartResult, error) {
	provider := s.samlProviders[payload.ProviderID]
	if provider == nil || s.repo == nil || s.mfaSecrets == nil {
		return nil, problem("unauthenticated")
	}
	request := &http.Request{Header: http.Header{"Cookie": []string{payload.Cookie}}}
	browser, err := request.Cookie(federationCookieName(payload.RelayState))
	if err != nil || len(browser.Value) != 64 {
		return nil, problem("unauthenticated")
	}
	flow, err := s.repo.q.IAMConsumeFederation(ctx, dbgen.IAMConsumeFederationParams{StateHash: tokenHash(payload.RelayState), BrowserHash: tokenHash(browser.Value)})
	if err != nil || flow.ProviderID != payload.ProviderID || flow.Issuer != provider.sp.IDPMetadata.EntityID {
		return nil, problem("unauthenticated")
	}
	requestID, err := s.mfaSecrets.Unseal("iam", "saml", payload.RelayState, flow.SealedVerifier)
	if err != nil || tokenHash(requestID) != flow.NonceHash {
		return nil, problem("unauthenticated")
	}
	xml, err := base64.StdEncoding.DecodeString(payload.SAMLResponse)
	if err != nil {
		return nil, problem("unauthenticated")
	}
	sp := provider.sp
	sp.ReplayCache = samlAssertionCache{ctx: ctx, queries: s.repo.q, provider: payload.ProviderID}
	assertion, err := sp.ParseXMLResponse(xml, []string{requestID}, sp.AcsURL)
	if err != nil || assertion.Subject == nil || assertion.Subject.NameID == nil {
		return nil, problem("unauthenticated")
	}
	name := assertion.Subject.NameID
	if name.Format != string(saml.PersistentNameIDFormat) || len(name.Value) == 0 || len(name.Value) > 2000 ||
		name.NameQualifier != "" && name.NameQualifier != flow.Issuer || name.SPNameQualifier != "" && name.SPNameQualifier != sp.EntityID {
		return nil, problem("unauthenticated")
	}
	completion, err := randomToken()
	if err != nil {
		return nil, safe(err)
	}
	if err = s.repo.q.IAMStartSAMLCompletion(ctx, dbgen.IAMStartSAMLCompletionParams{
		ProofHash: tokenHash(completion), ProviderID: flow.ProviderID, Issuer: flow.Issuer, Subject: name.Value, Purpose: flow.Purpose,
	}); err != nil {
		return nil, safe(err)
	}
	cookie := (&http.Cookie{Name: samlCompletionCookie, Value: completion, Path: "/api/auth/federation/saml", MaxAge: 120,
		HttpOnly: true, Secure: s.SecureCookies, SameSite: http.SameSiteLaxMode}).String()
	return &genidentity.FederationStartResult{AuthorizationURL: provider.returnURL, Cookie: cookie}, nil
}

func (s *Service) CompleteSaml(ctx context.Context, payload *genidentity.CompleteSamlPayload) (*genidentity.FederationCompleteResult, error) {
	if s.repo == nil {
		return nil, problem("unavailable")
	}
	request := &http.Request{Header: http.Header{"Cookie": []string{payload.Cookie}}}
	cookie, err := request.Cookie(samlCompletionCookie)
	if err != nil || len(cookie.Value) != 64 {
		return nil, problem("unauthenticated")
	}
	proof, err := s.repo.q.IAMConsumeSAMLCompletion(ctx, tokenHash(cookie.Value))
	if err != nil || s.samlProviders[proof.ProviderID] == nil || s.externalIssuer(proof.ProviderID) != proof.Issuer {
		return nil, problem("unauthenticated")
	}
	return s.completeExternalIdentity(ctx, proof.ProviderID, proof.Issuer, proof.Subject, proof.Purpose, payload.MfaCode, payload.RecoveryCode)
}
