//go:build integration

package tests

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/runtime/app"
	"horizon/foundation/system/governance"
	"horizon/foundation/system/identity"
)

type oidcTestCode struct{ nonce, challenge, mode string }

func TestIAMOIDCLinkLoginAndReplay(t *testing.T) {
	t.Run("identity ownership and replay", func(t *testing.T) { testIAMOIDC(t, false) })
	t.Run("local MFA and recovery replay", func(t *testing.T) { testIAMOIDC(t, true) })
}

func testIAMOIDC(t *testing.T, secondFactor bool) {
	_, pool, root := iamFixture(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var issuer *httptest.Server
	var mutex sync.Mutex
	codes := map[string]oidcTestCode{}
	issuer = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		var data any
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			data = map[string]any{"issuer": issuer.URL, "authorization_endpoint": issuer.URL + "/authorize", "token_endpoint": issuer.URL + "/token",
				"jwks_uri": issuer.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}}
		case "/keys":
			data = map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "fixture", "alg": "RS256", "use": "sig",
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB"}}}
		case "/token":
			if request.ParseForm() != nil {
				writer.WriteHeader(400)
				return
			}
			mutex.Lock()
			code, found := codes[request.Form.Get("code")]
			delete(codes, request.Form.Get("code"))
			mutex.Unlock()
			challenge := sha256.Sum256([]byte(request.Form.Get("code_verifier")))
			client, secret, basic := request.BasicAuth()
			if !found || !basic || client != "horizon-test" || secret != "isolated-provider-secret" ||
				base64.RawURLEncoding.EncodeToString(challenge[:]) != code.challenge || request.Form.Get("redirect_uri") != "http://127.0.0.1:3000/login/oidc" {
				writer.WriteHeader(400)
				return
			}
			claims := map[string]any{"iss": issuer.URL, "sub": "stable-subject-1", "aud": "horizon-test", "exp": time.Now().Add(time.Minute).Unix(),
				"iat": time.Now().Unix(), "nonce": code.nonce}
			if code.mode == "wrong-nonce" {
				claims["nonce"] = "another-flow"
			}
			if code.mode == "wrong-audience" {
				claims["aud"] = "another-client"
			}
			if code.mode == "expired" {
				claims["exp"] = time.Now().Add(-time.Minute).Unix()
			}
			header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"fixture","typ":"JWT"}`))
			payload, err := json.Marshal(claims)
			if err != nil {
				t.Error(err)
				writer.WriteHeader(500)
				return
			}
			signingInput := header + "." + base64.RawURLEncoding.EncodeToString(payload)
			digest := sha256.Sum256([]byte(signingInput))
			signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
			if err != nil {
				t.Error(err)
				writer.WriteHeader(500)
				return
			}
			data = map[string]any{"access_token": "isolated-access-token", "token_type": "Bearer", "expires_in": 60,
				"id_token": signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)}
		default:
			writer.WriteHeader(404)
			return
		}
		if err := json.NewEncoder(writer).Encode(data); err != nil {
			t.Error(err)
		}
	}))
	defer issuer.Close()
	iam := identity.New(pool, false)
	if err := iam.ConfigureOIDC(t.Context(), []identity.OIDCProviderConfig{{ID: "fixture", Name: "Isolated test provider", Issuer: issuer.URL,
		ClientID: "horizon-test", ClientSecret: "isolated-provider-secret", RedirectURL: "http://127.0.0.1:3000/login/oidc"}}, "http://127.0.0.1:3000"); err != nil {
		t.Fatal(err)
	}
	core := governance.New(pool, iam, governance.Options{EncryptionKey: []byte("01234567890123456789012345678901"), StorageRoot: t.TempDir(), PublicWorkspace: "tenant-a"})
	handler := deploymentHandler(app.System{}, core, iam)
	owner := loginTenantIAM(t, handler, root.Username, "tenant-a")
	federatedSession := func(handler http.Handler, login *httptest.ResponseRecorder, username string) iamClient {
		t.Helper()
		profile := decodeBody[genidentity.FederationCompleteResult](t, login).Profile
		cookies := login.Result().Cookies()
		if profile == nil || profile.Account == nil || profile.Account.Username != username ||
			profile.TenantID != "tenant-a" || len(cookies) != 1 {
			t.Fatalf("federation did not establish the authorized deployment session: %+v", profile)
		}
		return iamClient{handler: handler, cookie: cookies[0], csrf: profile.CsrfToken}
	}
	flowNumber := 0
	start := func(purpose, mode string) (iamClient, map[string]any) {
		t.Helper()
		response := (iamClient{handler: handler}).request(t, http.MethodPost, "/api/auth/federation/start", map[string]any{"providerId": "fixture", "purpose": purpose}, false)
		checkStatus(t, response, 200)
		result := decodeBody[genidentity.FederationStartResult](t, response)
		authorization, err := url.Parse(result.AuthorizationURL)
		if err != nil || authorization.Query().Get("code_challenge_method") != "S256" || authorization.Query().Get("nonce") == "" || authorization.Query().Get("state") == "" {
			t.Fatal("OIDC request omitted PKCE, state or nonce")
		}
		flowNumber++
		code := strings.Repeat("c", flowNumber)
		mutex.Lock()
		codes[code] = oidcTestCode{authorization.Query().Get("nonce"), authorization.Query().Get("code_challenge"), mode}
		mutex.Unlock()
		cookies := response.Result().Cookies()
		if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].MaxAge != 300 {
			t.Fatal("missing protected federation cookie")
		}
		return iamClient{handler: handler, cookie: cookies[0]}, map[string]any{"state": authorization.Query().Get("state"), "code": code}
	}
	complete := func(browser iamClient, payload map[string]any, status int) *httptest.ResponseRecorder {
		t.Helper()
		response := browser.request(t, http.MethodPost, "/api/auth/federation/complete", payload, false)
		checkStatus(t, response, status)
		return response
	}
	browser, payload := start("link", "valid")
	missingCookie := iamClient{handler: handler, cookie: &http.Cookie{Name: browser.cookie.Name, Value: strings.Repeat("0", 64)}}
	complete(missingCookie, payload, 401)
	proof := decodeBody[genidentity.FederationCompleteResult](t, complete(browser, payload, 200)).LinkProof
	if proof == nil || len(*proof) != 64 {
		t.Fatal("external ownership proof missing")
	}
	complete(browser, payload, 401)
	linked := checkIAMInvocation(t, owner.invoke(t, "identity.externalIdentity.link", map[string]any{"proof": *proof, "password": "TestOnly!2026Secure"}, true))
	if linked["linked"] != true || linked["reviewRequired"] != false {
		t.Fatalf("external link failed: %+v", linked)
	}
	checkStatus(t, owner.invoke(t, "identity.externalIdentity.link", map[string]any{"proof": *proof, "password": "TestOnly!2026Secure"}, true), 403)
	if secondFactor {
		enrollment := checkIAMInvocation(t, owner.invoke(t, "identity.mfa.enroll", map[string]any{"password": "TestOnly!2026Secure"}, true))
		secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enrollment["secret"].(string))
		if err != nil {
			t.Fatal(err)
		}
		var counter int64
		if err = pool.QueryRow(t.Context(), `SELECT floor(extract(epoch FROM statement_timestamp()) / 30)::bigint`).Scan(&counter); err != nil {
			t.Fatal(err)
		}
		var timestamp [8]byte
		binary.BigEndian.PutUint64(timestamp[:], uint64(counter))
		mac := hmac.New(sha1.New, secret)
		_, _ = mac.Write(timestamp[:])
		sum := mac.Sum(nil)
		offset := int(sum[len(sum)-1] & 15)
		code := fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[offset:offset+4])&0x7fffffff)%1000000)
		issued := checkIAMInvocation(t, owner.invoke(t, "identity.mfa.activate", map[string]any{"code": code}, true))
		recovery := issued["recoveryCodes"].([]any)[0]
		for _, attempt := range []struct {
			factor map[string]any
			status int
		}{
			{map[string]any{}, 401},
			{map[string]any{"mfaCode": code}, 401},
			{map[string]any{"recoveryCode": recovery}, 200},
			{map[string]any{"recoveryCode": recovery}, 401},
		} {
			browser, payload := start("login", "valid")
			for field, value := range attempt.factor {
				payload[field] = value
			}
			complete(browser, payload, attempt.status)
		}
		return
	}
	for _, mode := range []string{"wrong-nonce", "wrong-audience", "expired"} {
		browser, payload = start("login", mode)
		complete(browser, payload, 401)
	}
	browser, payload = start("login", "valid")
	if _, err := pool.Exec(t.Context(), `UPDATE iam_federation_flows SET expires_at = now() - interval '1 second' WHERE consumed_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	complete(browser, payload, 401)
	browser, payload = start("login", "valid")
	login := complete(browser, payload, 200)
	external := federatedSession(handler, login, root.Username)
	checkStatus(t, external.request(t, http.MethodGet, "/api/auth/me", nil, false), 200)
	conflictAccount := checkIAMInvocation(t, owner.invoke(t, "identity.account.create", map[string]any{
		"username": "external-conflict", "fullName": "Conflicting local owner", "initialStatus": "active", "password": "TestOnly!2026Secure",
	}, true))
	seedTenantRole(t, pool, "tenant-a", "external-conflict", root.ID, int64(conflictAccount["id"].(float64)), []*genidentity.PermissionGrant{
		{Permission: "example:read", Effect: "allow", Scope: "ALL"},
	})
	other := loginTenantIAM(t, handler, "external-conflict", "tenant-a")
	browser, payload = start("link", "valid")
	proof = decodeBody[genidentity.FederationCompleteResult](t, complete(browser, payload, 200)).LinkProof
	conflict := checkIAMInvocation(t, other.invoke(t, "identity.externalIdentity.link", map[string]any{"proof": *proof, "password": "TestOnly!2026Secure"}, true))
	if conflict["linked"] != false || conflict["reviewRequired"] != true {
		t.Fatalf("external identity was stolen by a second local account: %+v", conflict)
	}
	review := map[string]any{"id": conflict["reviewId"]}
	checkStatus(t, other.invoke(t, "identity.externalReview.confirm", review, true), 403)
	checkStatus(t, owner.invoke(t, "identity.externalReview.confirm", review, true), 403)
	checkIAMInvocation(t, external.invoke(t, "identity.externalReview.confirm", review, true))
	checkStatus(t, owner.invoke(t, "identity.externalReview.decide", map[string]any{
		"id": review["id"], "decision": "apply", "reason": "Reject a review by the original owner",
	}, true), 403)
	createdReviewer := checkIAMInvocation(t, owner.invoke(t, "identity.account.create", map[string]any{
		"username": "external-reviewer", "fullName": "Independent reviewer", "initialStatus": "active", "password": "TestOnly!2026Secure",
	}, true))
	seedTenantRole(t, pool, "tenant-a", "external-reviewer", root.ID, int64(createdReviewer["id"].(float64)), []*genidentity.PermissionGrant{
		{Permission: "access:manage", Effect: "allow", Scope: "ALL"},
	})
	reviewer := loginTenantIAM(t, handler, "external-reviewer", "tenant-a")
	decide := func(decision string) {
		t.Helper()
		checkIAMInvocation(t, reviewer.invoke(t, "identity.externalReview.decide", map[string]any{
			"id": review["id"], "decision": decision, "reason": "Independent external identity review",
		}, true))
	}
	decide("apply")
	checkStatus(t, external.request(t, http.MethodGet, "/api/auth/me", nil, false), 401)
	handler = deploymentHandler(app.System{}, core, iam)
	browser, payload = start("login", "valid")
	transferred := complete(browser, payload, 200)
	transferredSession := federatedSession(handler, transferred, "external-conflict")
	decide("rollback")
	checkStatus(t, transferredSession.request(t, http.MethodGet, "/api/auth/me", nil, false), 401)
	var conflictCount int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM cap_outbox WHERE event_type = 'identity.externalIdentity.conflict'`).Scan(&conflictCount); err != nil || conflictCount != 1 {
		t.Fatalf("external ownership conflict was not audited: %v", err)
	}
	checkIAMInvocation(t, owner.invoke(t, "identity.externalIdentity.unlink", map[string]any{"id": linked["id"], "password": "TestOnly!2026Secure"}, true))
	checkStatus(t, external.request(t, http.MethodGet, "/api/auth/me", nil, false), 401)
	checkStatus(t, owner.request(t, http.MethodGet, "/api/auth/me", nil, false), 200)
	browser, payload = start("link", "valid")
	proof = decodeBody[genidentity.FederationCompleteResult](t, complete(browser, payload, 200)).LinkProof
	relinked := checkIAMInvocation(t, owner.invoke(t, "identity.externalIdentity.link", map[string]any{"proof": *proof, "password": "TestOnly!2026Secure"}, true))
	if relinked["linked"] != true || relinked["id"] != linked["id"] {
		t.Fatal("relinking did not preserve the original ownership mapping")
	}
	checkStatus(t, external.request(t, http.MethodGet, "/api/auth/me", nil, false), 401)
	var leaked bool
	if err := pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM cap_invocations WHERE input::text LIKE '%stable-subject-1%' OR coalesce(result::text, '') LIKE '%stable-subject-1%')`).Scan(&leaked); err != nil || leaked {
		t.Fatal("external subject leaked into invocation projections")
	}
	checkStatus(t, owner.invoke(t, "identity.externalIdentity.requireExternal", map[string]any{"confirm": true}, true), 403)
	// Run the passwordless scenario through an independent HTTP limiter. The
	// first scenario already exercises several rejected provider handshakes.
	handler = deploymentHandler(app.System{}, core, iam)
	browser, payload = start("login", "valid")
	fresh := complete(browser, payload, 200)
	external = federatedSession(handler, fresh, root.Username)
	checkIAMInvocation(t, external.invoke(t, "identity.externalIdentity.requireExternal", map[string]any{"confirm": true}, true))
	checkStatus(t, owner.request(t, http.MethodGet, "/api/auth/me", nil, false), 401)
	checkStatus(t, external.request(t, http.MethodGet, "/api/auth/me", nil, false), 401)
	checkStatus(t, (iamClient{handler: handler}).request(t, "POST", "/api/auth/login",
		map[string]string{"username": root.Username, "password": "TestOnly!2026Secure"}, false), 401)
	browser, payload = start("login", "valid")
	complete(browser, payload, 200)
	if _, err := pool.Exec(t.Context(), `UPDATE iam_tenant_memberships SET status='suspended'
WHERE principal_id=(SELECT principal_id FROM iam_login_accounts WHERE username=$1)`, root.Username); err != nil {
		t.Fatal(err)
	}
	browser, payload = start("login", "valid")
	unscoped := complete(browser, payload, 200)
	preWorkspace := decodeBody[genidentity.FederationCompleteResult](t, unscoped).Profile
	if preWorkspace.Account != nil || preWorkspace.TenantID != "" {
		t.Fatalf("external login with no active membership leaked a work context: %+v", preWorkspace)
	}
}
