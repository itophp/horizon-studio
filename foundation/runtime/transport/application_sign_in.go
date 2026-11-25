package transport

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"horizon/foundation/runtime/app"
	"horizon/foundation/system/identity"
)

const applicationSignInPrefix = "/api/auth/applications/"

var signInName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

type applicationSignIns struct {
	namespaces map[string]string
	defaultID  string
}

func newApplicationSignIns(registration app.Registration) applicationSignIns {
	entries := applicationSignIns{namespaces: make(map[string]string), defaultID: registration.DefaultSignIn}
	used := make(map[string]bool)
	for _, source := range registration.SignInApplications {
		if !signInName.MatchString(source.ID) || !signInName.MatchString(source.Namespace) ||
			entries.namespaces[source.ID] != "" || used[source.Namespace] {
			panic(fmt.Errorf("invalid or duplicate application authentication entry %q", source.ID))
		}
		entries.namespaces[source.ID] = source.Namespace
		for _, alias := range source.Aliases {
			if !signInName.MatchString(alias) || entries.namespaces[alias] != "" {
				panic(fmt.Errorf("invalid or duplicate authentication alias %q", alias))
			}
			entries.namespaces[alias] = source.Namespace
		}
		used[source.Namespace] = true
	}
	if len(entries.namespaces) > 0 && registration.Authentication == nil {
		panic("application authentication handler is missing")
	}
	if entries.defaultID != "" && entries.namespaces[entries.defaultID] == "" {
		panic("default application authentication entry is missing")
	}
	return entries
}

func publicCredentialAction(path string) bool {
	switch path {
	case "/api/auth/login", "/api/auth/registration/request", "/api/auth/registration/complete",
		"/api/auth/password-reset/request", "/api/auth/password-reset/complete":
		return true
	default:
		return false
	}
}

// bind resolves the route against deployment-owned applications, then sends
// the request through the same Goa decoder and IAM guards as the default app.
func (entries applicationSignIns) bind(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	id, path := entries.defaultID, r.URL.Path
	if strings.HasPrefix(path, "/api/auth/federation/") {
		if id == "" {
			deny(w, http.StatusNotFound, "not_found")
			return r, false
		}
		return r.WithContext(identity.WithApplicationAuthentication(r.Context(), entries.namespaces[id])), true
	}
	if tail, named := strings.CutPrefix(path, applicationSignInPrefix); named {
		application, action, found := strings.Cut(tail, "/")
		id, path = application, "/api/auth/"+action
		if !found || !publicCredentialAction(path) || r.Method != http.MethodPost {
			deny(w, http.StatusNotFound, "not_found")
			return r, false
		}
	}
	if !publicCredentialAction(path) || r.Method != http.MethodPost {
		return r, true
	}
	namespace := entries.namespaces[id]
	if namespace == "" {
		deny(w, http.StatusNotFound, "not_found")
		return r, false
	}
	r = r.Clone(identity.WithApplicationAuthentication(r.Context(), namespace))
	r.URL.Path, r.URL.RawPath = path, ""
	return r, true
}
