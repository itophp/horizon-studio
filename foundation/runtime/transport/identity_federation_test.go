package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	goahttp "goa.design/goa/v3/http"
	"horizon/foundation/system/identity"
)

func TestFederationOriginExceptionIsLimitedToSAMLFormPost(t *testing.T) {
	const acs = "/api/auth/federation/saml/fixture/acs"
	body := "RelayState=" + strings.Repeat("a", 64) + "&SAMLResponse=encoded"
	for _, tc := range []struct {
		name, method, path, body, contentType string
		allowed                               bool
		status                                int
	}{
		{"IdP form post", "POST", acs, body, "application/x-www-form-urlencoded", true, 401},
		{"duplicate assertion", "POST", acs, body + "&SAMLResponse=second", "application/x-www-form-urlencoded", true, 400},
		{"duplicate state", "POST", acs, body + "&RelayState=" + strings.Repeat("b", 64), "application/x-www-form-urlencoded", true, 400},
		{"query assertion", "POST", acs + "?SAMLResponse=second", body, "application/x-www-form-urlencoded", false, 400},
		{"query state", "POST", acs + "?RelayState=" + strings.Repeat("b", 64), body, "application/x-www-form-urlencoded", false, 400},
		{"JSON", "POST", acs, `{}`, "application/json", true, 400},
		{"malformed form", "POST", acs, body + "%ZZ", "application/x-www-form-urlencoded", true, 400},
		{"completion", "POST", "/api/auth/federation/saml/complete", `{}`, "application/json", false, 403},
		{"OIDC completion", "POST", "/api/auth/federation/complete", `{}`, "application/json", false, 403},
		{"start", "POST", "/api/auth/federation/start", `{}`, "application/json", false, 403},
		{"login", "POST", "/api/auth/login", `{}`, "application/json", false, 403},
		{"suffix", "POST", acs + "/extra", body, "application/x-www-form-urlencoded", false, 403},
		{"other method", "PUT", acs, body, "application/x-www-form-urlencoded", false, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := goahttp.NewMuxer()
			guard := mountIdentity(mux, identity.New(nil, true), nil, func(context.Context, http.ResponseWriter, error) { t.Fatal("unexpected encoder call") })
			request := httptest.NewRequest(tc.method, "https://console.test"+tc.path, strings.NewReader(tc.body))
			request.AddCookie(&http.Cookie{Name: "test", Value: "test"})
			request.Header.Set("Origin", "https://idp.test")
			request.Header.Set("Content-Type", tc.contentType)
			response := httptest.NewRecorder()
			next, allowed := guard(response, request)
			if allowed {
				mux.ServeHTTP(response, next)
			}
			if allowed != tc.allowed || response.Code != tc.status {
				t.Fatalf("allowed=%v status=%d body=%s", allowed, response.Code, response.Body.String())
			}
		})
	}
}
