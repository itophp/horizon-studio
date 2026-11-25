package transport

import (
	"context"
	"errors"
	goahttp "goa.design/goa/v3/http"
	identityhttp "horizon/foundation/gen/http/identity/server"
	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/runtime/app"
	"horizon/foundation/system/identity"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type loginBucket struct {
	window time.Time
	count  int
}
type loginLimiter struct {
	mu      sync.Mutex
	buckets map[string]loginBucket
}

func (l *loginLimiter) allow(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket := l.buckets[host]
	if now.Sub(bucket.window) >= time.Minute {
		bucket = loginBucket{window: now}
	}
	bucket.count++
	if len(l.buckets) >= 4096 {
		for key, value := range l.buckets {
			if now.Sub(value.window) > time.Minute {
				delete(l.buckets, key)
			}
		}
		if len(l.buckets) >= 4096 {
			return false
		}
	}
	l.buckets[host] = bucket
	return bucket.count <= 20
}
func sameOrigin(r *http.Request, secure bool) bool {
	origin, err := url.Parse(r.Header.Get("Origin"))
	scheme := "http"
	if secure || r.TLS != nil {
		scheme = "https"
	}
	return err == nil && origin.Scheme == scheme && strings.EqualFold(origin.Host, r.Host) && origin.User == nil && (origin.Path == "" || origin.Path == "/") && origin.RawQuery == "" && origin.Fragment == ""
}
func identityStatus(err error) int {
	var problem *genidentity.IAMProblem
	if errors.As(err, &problem) {
		switch problem.Code {
		case "unauthenticated":
			return 401
		case "forbidden":
			return 403
		case "unavailable":
			return 503
		}
	}
	return 500
}

// guardedPath lists the session-scoped API surfaces. The guard attaches the
// trusted Principal and enforces Origin/CSRF for unsafe methods; a path outside
// these prefixes reaches its handler without an authenticated principal.
func guardedPath(path string) bool {
	return strings.HasPrefix(path, "/api/admin/") ||
		strings.HasPrefix(path, "/api/auth/") ||
		strings.HasPrefix(path, "/api/assistant/") ||
		strings.HasPrefix(path, "/api/ai/") ||
		strings.HasPrefix(path, "/api/capabilities")
}

type applicationIdentityEndpoints struct {
	*identity.Service
	app.ApplicationAuthentication
}

func mountIdentity(mux goahttp.Muxer, service *identity.Service, authentication app.ApplicationAuthentication, onError func(context.Context, http.ResponseWriter, error)) func(http.ResponseWriter, *http.Request) (*http.Request, bool) {
	adapter := applicationIdentityEndpoints{Service: service, ApplicationAuthentication: authentication}
	identityhttp.Mount(mux, identityhttp.New(genidentity.NewEndpoints(adapter), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, onError, nil))
	limiter := &loginLimiter{buckets: map[string]loginBucket{}}
	return func(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
		if !guardedPath(r.URL.Path) {
			return r, true
		}
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/capabilities/") && strings.HasSuffix(r.URL.Path, "/machine-invocations") {
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) == 5 && parts[3] != "" {
				scheme, secret, ok := strings.Cut(r.Header.Get("Authorization"), " ")
				if !ok || !strings.EqualFold(scheme, "Bearer") || len(r.Header.Values("Authorization")) != 1 || len(r.Header.Values("X-Horizon-Delegation")) > 1 {
					deny(w, 401, "unauthenticated")
					return r, false
				}
				principal, err := service.AuthenticateMachine(r.Context(), secret, r.Header.Get("X-Horizon-Delegation"))
				if err != nil {
					status := identityStatus(err)
					code := "internal"
					if status == 401 {
						code = "unauthenticated"
					} else if status == 403 {
						code = "forbidden"
					} else if status == 503 {
						code = "unavailable"
					}
					deny(w, status, code)
					return r, false
				}
				return r.WithContext(identity.WithPrincipal(r.Context(), principal)), true
			}
		}
		// Only the SAML protocol's exact ACS POST accepts a cross-site IdP.
		// The service verifies the signed request correlation and one-use browser
		// cookie before creating a completion. Other auth endpoints keep Origin.
		parts := strings.Split(r.URL.Path, "/")
		if r.Method == http.MethodPost && len(parts) == 7 && parts[1] == "api" && parts[2] == "auth" &&
			parts[3] == "federation" && parts[4] == "saml" && parts[5] != "" && parts[6] == "acs" {
			if !limiter.allow(r.RemoteAddr) {
				deny(w, 429, "rate_limited")
				return r, false
			}
			// Protocol fields belong exclusively to the native Goa form body.
			// Its generated decoder rejects duplicate fields without consuming
			// the body here, before the shared request-size limit is installed.
			query, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil || query.Has("SAMLResponse") || query.Has("RelayState") {
				deny(w, 400, "invalid")
				return r, false
			}
			return r, true
		}
		unsafe := r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS"
		if unsafe && !sameOrigin(r, service.SecureCookies) {
			deny(w, 403, "forbidden")
			return r, false
		}
		if r.URL.Path == "/api/auth/federation/providers" && r.Method == http.MethodGet {
			return r, true
		}
		if r.URL.Path == "/api/auth/login" || r.Method == http.MethodPost &&
			(r.URL.Path == "/api/auth/federation/start" || r.URL.Path == "/api/auth/federation/complete" || r.URL.Path == "/api/auth/federation/saml/complete" ||
				r.URL.Path == "/api/auth/password-reset/request" || r.URL.Path == "/api/auth/password-reset/complete" ||
				r.URL.Path == "/api/auth/registration/request" || r.URL.Path == "/api/auth/registration/complete") {
			if !limiter.allow(r.RemoteAddr) {
				deny(w, 429, "rate_limited")
				return r, false
			}
			return r.WithContext(identity.WithLoginDevice(r.Context(), r.UserAgent())), true
		}
		cookie, err := r.Cookie(identity.CookieName)
		if err != nil {
			deny(w, 401, "unauthenticated")
			return r, false
		}
		p, err := service.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			status := identityStatus(err)
			code := "internal"
			if status == 401 {
				code = "unauthenticated"
			} else if status == 503 {
				code = "unavailable"
			}
			deny(w, status, code)
			return r, false
		}
		if (p.IsUnscopedLogin() && !unscopedLoginPath(r.Method, r.URL.Path)) ||
			(!p.IsUnscopedLogin() && p.TenantID != identity.InteractiveTenant(r.Context())) {
			deny(w, 401, "unauthenticated")
			return r, false
		}
		if unsafe && !identity.CSRFMatches(p, r.Header.Get("X-CSRF-Token")) {
			deny(w, 403, "forbidden")
			return r, false
		}
		if contextNonce := r.Header.Get("X-Horizon-Context"); contextNonce != "" && !identity.CSRFMatches(p, contextNonce) {
			deny(w, 401, "unauthenticated")
			return r, false
		}
		return r.WithContext(identity.WithPrincipal(r.Context(), p)), true
	}
}

func unscopedLoginPath(method, path string) bool {
	switch method {
	case http.MethodGet:
		return path == "/api/auth/me" || path == "/api/auth/identities"
	case http.MethodPost:
		return path == "/api/auth/identity" || path == "/api/auth/invitation" || path == "/api/auth/logout"
	default:
		return false
	}
}
