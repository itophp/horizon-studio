//go:build integration

package tests

import (
	"bytes"
	"compress/flate"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	dsig "github.com/russellhaering/goxmldsig"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/runtime/app"
	runtimeconfig "horizon/foundation/runtime/config"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
)

func samlTestCertificate(t *testing.T) (*rsa.PrivateKey, *x509.Certificate, string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Isolated SAML test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return key, certificate, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

func TestIAMSAMLLinkLoginAndReplay(t *testing.T) {
	_, pool, root := iamFixture(t)
	idpKey, idpCertificate, _, _ := samlTestCertificate(t)
	spKey, _, spCertificate, spPrivateKey := samlTestCertificate(t)
	const issuer = "https://isolated-idp.test/metadata"
	const entity = "https://127.0.0.1:3000/saml/entity"
	const acs = "https://127.0.0.1:3000/api/auth/federation/saml/fixture/acs"
	metadata := fmt.Sprintf(`<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="%s">
	<IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
	<KeyDescriptor use="signing"><KeyInfo xmlns="http://www.w3.org/2000/09/xmldsig#"><X509Data><X509Certificate>%s</X509Certificate></X509Data></KeyInfo></KeyDescriptor>
	<SingleSignOnService Binding="%s" Location="https://isolated-idp.test/sso"/>
	</IDPSSODescriptor></EntityDescriptor>`, issuer, base64.StdEncoding.EncodeToString(idpCertificate.Raw), saml.HTTPRedirectBinding)
	config := identity.SAMLProviderConfig{ID: "fixture", Name: "SAML fixture", EntityID: entity,
		MetadataXML: metadata, CertificatePEM: spCertificate, PrivateKeyPEM: spPrivateKey}

	t.Run("deployment configuration", func(t *testing.T) {
		t.Setenv("API_ADDR", "127.0.0.1:8080")
		t.Setenv("COOKIE_SECURE", "true")
		t.Setenv("PUBLIC_APP_URL", "https://127.0.0.1:3000")
		t.Setenv("IAM_OIDC_PROVIDERS_JSON", "[]")
		t.Setenv("SETTINGS_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
		encoded, err := json.Marshal([]identity.SAMLProviderConfig{config})
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("IAM_SAML_PROVIDERS_JSON", string(encoded))
		if _, err := runtimeconfig.Load(); err != nil {
			t.Fatal(err)
		}
		t.Setenv("SETTINGS_ENCRYPTION_KEY", "")
		if _, err := runtimeconfig.Load(); err == nil {
			t.Fatal("SAML deployment accepted without verifier encryption key")
		}
		t.Setenv("SETTINGS_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
		oidc, err := json.Marshal([]identity.OIDCProviderConfig{{ID: config.ID, Name: "OIDC", Issuer: "https://idp.test", ClientID: "test", RedirectURL: "https://127.0.0.1:3000/login/oidc"}})
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("IAM_OIDC_PROVIDERS_JSON", string(oidc))
		if _, err := runtimeconfig.Load(); err == nil {
			t.Fatal("deployment accepted duplicate cross-protocol provider IDs")
		}
	})
	iam := identity.New(pool, false)
	if err := iam.ConfigureSAML([]identity.SAMLProviderConfig{config}, "https://127.0.0.1:3000"); err != nil {
		t.Fatal(err)
	}
	core := governance.New(pool, iam, governance.Options{EncryptionKey: []byte("01234567890123456789012345678901"), StorageRoot: t.TempDir(), PublicWorkspace: "tenant-a"})
	handler := deploymentHandler(app.System{}, core, iam)
	owner := loginTenantIAM(t, handler, root.Username, "tenant-a")
	posts := 0
	post := func(path string, payload map[string]any, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		posts++
		request := httptest.NewRequest("POST", "http://console.test"+path, bytes.NewReader(encoded))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://console.test")
		request.RemoteAddr = fmt.Sprintf("198.51.100.%d:4000", posts)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	providers := decodeBody[[]genidentity.FederationProvider](t, owner.request(t, "GET", "/api/auth/federation/providers", nil, false))
	if len(providers) != 1 || providers[0].ID != "fixture" {
		t.Fatal("SAML provider absent from the common external identity catalog")
	}

	type flow struct {
		state, requestID string
		cookie           *http.Cookie
	}
	start := func(purpose string) flow {
		t.Helper()
		response := post("/api/auth/federation/start", map[string]any{"providerId": "fixture", "purpose": purpose}, nil)
		checkStatus(t, response, 200)
		redirect, err := url.Parse(decodeBody[genidentity.FederationStartResult](t, response).AuthorizationURL)
		if err != nil {
			t.Fatal(err)
		}
		query := redirect.Query()
		signature, err := base64.StdEncoding.DecodeString(query.Get("Signature"))
		if err != nil {
			t.Fatal(err)
		}
		signed := "SAMLRequest=" + url.QueryEscape(query.Get("SAMLRequest")) + "&RelayState=" + url.QueryEscape(query.Get("RelayState")) + "&SigAlg=" + url.QueryEscape(query.Get("SigAlg"))
		digest := sha256.Sum256([]byte(signed))
		if query.Get("SigAlg") != dsig.RSASHA256SignatureMethod || rsa.VerifyPKCS1v15(&spKey.PublicKey, crypto.SHA256, digest[:], signature) != nil {
			t.Fatal("SP authentication redirect is not correctly signed")
		}
		compressed, err := base64.StdEncoding.DecodeString(query.Get("SAMLRequest"))
		if err != nil {
			t.Fatal(err)
		}
		reader := flate.NewReader(bytes.NewReader(compressed))
		defer reader.Close()
		raw, err := io.ReadAll(io.LimitReader(reader, 32768))
		var request saml.AuthnRequest
		if err != nil || xml.Unmarshal(raw, &request) != nil || request.AssertionConsumerServiceURL != acs || request.ProtocolBinding != saml.HTTPPostBinding {
			t.Fatal("invalid SP-initiated request")
		}
		cookies := response.Result().Cookies()
		if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteNoneMode {
			t.Fatal("SAML flow cookie does not support a protected cross-site IdP POST")
		}
		return flow{query.Get("RelayState"), request.ID, cookies[0]}
	}
	responseFor := func(flow flow, assertionID, mode string) string {
		t.Helper()
		now := time.Now().UTC()
		assertion := &saml.Assertion{ID: assertionID, Version: "2.0", IssueInstant: now, Issuer: saml.Issuer{Value: issuer},
			Subject: &saml.Subject{NameID: &saml.NameID{Format: string(saml.PersistentNameIDFormat), Value: "stable-saml-subject", NameQualifier: issuer, SPNameQualifier: entity},
				SubjectConfirmations: []saml.SubjectConfirmation{{Method: "urn:oasis:names:tc:SAML:2.0:cm:bearer", SubjectConfirmationData: &saml.SubjectConfirmationData{
					InResponseTo: flow.requestID, Recipient: acs, NotOnOrAfter: now.Add(time.Minute),
				}}}},
			Conditions: &saml.Conditions{NotBefore: now.Add(-time.Minute), NotOnOrAfter: now.Add(time.Minute),
				AudienceRestrictions: []saml.AudienceRestriction{{Audience: saml.Audience{Value: entity}}}},
			AuthnStatements: []saml.AuthnStatement{{AuthnInstant: now, SessionIndex: "isolated-session",
				AuthnContext: saml.AuthnContext{AuthnContextClassRef: &saml.AuthnContextClassRef{Value: "urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport"}}}},
		}
		response := &saml.Response{ID: "response-" + assertionID, InResponseTo: flow.requestID, Version: "2.0", IssueInstant: now, Destination: acs,
			Issuer: &saml.Issuer{Value: issuer}, Status: saml.Status{StatusCode: saml.StatusCode{Value: saml.StatusSuccess}}}
		switch mode {
		case "audience":
			assertion.Conditions.AudienceRestrictions[0].Audience.Value = "https://other-sp.test/"
		case "expired":
			assertion.Conditions.NotOnOrAfter = now.Add(-10 * time.Minute)
		case "request":
			assertion.Subject.SubjectConfirmations[0].SubjectConfirmationData.InResponseTo = "other-request"
		case "destination":
			response.Destination = "https://other-sp.test/acs"
		case "recipient":
			assertion.Subject.SubjectConfirmations[0].SubjectConfirmationData.Recipient = "https://other-sp.test/acs"
		case "issuer":
			assertion.Issuer.Value = "https://other-idp.test/"
		case "transient":
			assertion.Subject.NameID.Format = string(saml.TransientNameIDFormat)
		}
		signing, err := dsig.NewSigningContext(idpKey, [][]byte{idpCertificate.Raw})
		if err != nil {
			t.Fatal(err)
		}
		signing.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
		method := dsig.RSASHA256SignatureMethod
		if mode == "sha1" {
			method = dsig.RSASHA1SignatureMethod
		}
		if err = signing.SetSignatureMethod(method); err != nil {
			t.Fatal(err)
		}
		element, err := signing.SignEnveloped(assertion.Element())
		if err != nil {
			t.Fatal(err)
		}
		if mode == "unsigned" {
			element = assertion.Element()
		}
		if mode == "tampered" {
			element.FindElement("./Subject/NameID").SetText("tampered-subject")
		}
		responseElement := response.Element()
		responseElement.AddChild(element)
		if mode == "wrapped" {
			responseElement.AddChild(assertion.Element())
		}
		document := etree.NewDocument()
		document.SetRoot(responseElement)
		encoded, err := document.WriteToBytes()
		if err != nil {
			t.Fatal(err)
		}
		return base64.StdEncoding.EncodeToString(encoded)
	}
	requests := 0
	callback := func(flow flow, encoded string, status int) *httptest.ResponseRecorder {
		t.Helper()
		requests++
		request := httptest.NewRequest("POST", "http://console.test/api/auth/federation/saml/fixture/acs",
			strings.NewReader(url.Values{"RelayState": {flow.state}, "SAMLResponse": {encoded}}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Origin", "https://isolated-idp.test")
		request.RemoteAddr = fmt.Sprintf("192.0.2.%d:4000", requests+10)
		request.AddCookie(flow.cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		checkStatus(t, response, status)
		return response
	}
	complete := func(cookie *http.Cookie, factor map[string]any, status int) *httptest.ResponseRecorder {
		t.Helper()
		returnResponse := post("/api/auth/federation/saml/complete", factor, cookie)
		checkStatus(t, returnResponse, status)
		return returnResponse
	}
	finishFlow := func(purpose, assertionID string) *http.Cookie {
		t.Helper()
		flow := start(purpose)
		response := callback(flow, responseFor(flow, assertionID, "valid"), 303)
		if response.Header().Get("Location") != "https://127.0.0.1:3000/login/saml" || response.Body.Len() != 0 {
			t.Fatal("callback leaked a credential/assertion into the redirect or body")
		}
		return response.Result().Cookies()[0]
	}
	initial := start("link")
	encoded := responseFor(initial, "initial-assertion", "valid")
	wrongBrowser := initial
	wrongBrowser.cookie = &http.Cookie{Name: initial.cookie.Name, Value: strings.Repeat("0", 64)}
	callback(wrongBrowser, encoded, 401)
	first := callback(initial, encoded, 303)
	callback(initial, encoded, 401)
	proof := decodeBody[genidentity.FederationCompleteResult](t, complete(first.Result().Cookies()[0], map[string]any{}, 200)).LinkProof
	if proof == nil {
		t.Fatal("SAML ownership proof missing")
	}
	complete(first.Result().Cookies()[0], map[string]any{}, 401)
	linked := checkIAMInvocation(t, owner.invoke(t, "identity.externalIdentity.link", map[string]any{"proof": *proof, "password": "TestOnly!2026Secure"}, true))
	if linked["linked"] != true {
		t.Fatal("SAML identity could not be linked after verifying both accounts")
	}
	login := complete(finishFlow("login", "login-assertion"), map[string]any{}, 200)
	profile := decodeBody[genidentity.FederationCompleteResult](t, login).Profile
	if profile == nil || profile.Account == nil || profile.Account.ID != root.ID || profile.TenantID != "tenant-a" {
		t.Fatal("SAML login did not establish the original account session")
	}
	samlSession := iamClient{handler: handler, cookie: login.Result().Cookies()[0], csrf: profile.CsrfToken}

	for _, mode := range []string{"audience", "expired", "request", "destination", "recipient", "issuer", "transient", "unsigned", "tampered", "wrapped", "sha1"} {
		t.Run(mode, func(t *testing.T) {
			flow := start("login")
			callback(flow, responseFor(flow, "rejected-"+mode, mode), 401)
		})
	}
	// A new request with a correctly re-signed assertion still cannot reuse an
	// assertion ID already consumed by another instance/request.
	replay := start("login")
	callback(replay, responseFor(replay, "login-assertion", "valid"), 401)
	checkIAMInvocation(t, owner.invoke(t, "identity.externalIdentity.unlink", map[string]any{"id": linked["id"], "password": "TestOnly!2026Secure"}, true))
	checkStatus(t, samlSession.request(t, "GET", "/api/auth/me", nil, false), 401)
	checkStatus(t, owner.request(t, "GET", "/api/auth/me", nil, false), 200)

	// Re-link, enable local MFA, and prove SAML cannot bypass it or reuse a
	// recovery code. The same MFA service is shared with password and OIDC.
	linkProof := decodeBody[genidentity.FederationCompleteResult](t, complete(finishFlow("link", "relink-assertion"), map[string]any{}, 200)).LinkProof
	checkIAMInvocation(t, owner.invoke(t, "identity.externalIdentity.link", map[string]any{"proof": *linkProof, "password": "TestOnly!2026Secure"}, true))
	enrollment := checkIAMInvocation(t, owner.invoke(t, "identity.mfa.enroll", map[string]any{"password": "TestOnly!2026Secure"}, true))
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enrollment["secret"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var timestamp [8]byte
	binary.BigEndian.PutUint64(timestamp[:], uint64(time.Now().Unix()/30))
	mac := hmac.New(sha1.New, secret)
	if _, err = mac.Write(timestamp[:]); err != nil {
		t.Fatal(err)
	}
	sum := mac.Sum(nil)
	offset := int(sum[len(sum)-1] & 15)
	code := fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[offset:offset+4])&0x7fffffff)%1000000)
	recovery := checkIAMInvocation(t, owner.invoke(t, "identity.mfa.activate", map[string]any{"code": code}, true))["recoveryCodes"].([]any)[0]
	complete(finishFlow("login", "mfa-missing"), map[string]any{}, 401)
	complete(finishFlow("login", "mfa-recovery"), map[string]any{"recoveryCode": recovery}, 200)
	complete(finishFlow("login", "mfa-replayed"), map[string]any{"recoveryCode": recovery}, 401)
	var leaked bool
	if err := pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM cap_invocations
		WHERE input::text LIKE '%stable-saml-subject%' OR result::text LIKE '%stable-saml-subject%' OR result::text LIKE '%' || $1 || '%')`, *proof).Scan(&leaked); err != nil || leaked {
		t.Fatal("SAML subject or proof leaked into capability history", err)
	}
}
