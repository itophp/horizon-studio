package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"goa.design/goa/v3/security"
	genidentity "horizon/foundation/gen/identity"
	"horizon/foundation/kernel/capability/registry"
	"horizon/internal/repository/dbgen"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const CookieName = "baseline_session"

type Service struct {
	repo                            *Repository
	SecureCookies                   bool
	Policies                        PolicyProvider
	pluginCatalog                   []*genidentity.PermissionDefinition
	platformOperators               map[string]bool
	resourceAttributes              map[string]ResourceAttributeProvider
	authorizationCatalog            *registry.Registry
	applicationDirectoryReader      ApplicationDirectoryReader
	applicationAccountProfileWriter AccountProfileWriter
	contactDelivery                 map[string]ContactDelivery
	analyticsInvoke                 analyticsInvoke
	bindings                        bindingProviders
	organizationContexts            map[string]OrganizationContextResolver
	tenantProvisioning              tenantProvisioning
	mfaSecrets                      SecretProtector
	oidcProviders                   map[string]*oidcProvider
	samlProviders                   map[string]*samlProvider
}

func New(pool *pgxpool.Pool, secure bool) *Service {
	if pool == nil {
		return &Service{SecureCookies: secure}
	}
	return &Service{repo: newRepository(pool), SecureCookies: secure}
}
func problem(code string) error { return &genidentity.IAMProblem{Code: code} }

// MapStorageError converts repository failures into stable security API errors.
func MapStorageError(err error) error { return safe(err) }

func safe(err error) error {
	if err == nil {
		return nil
	}
	var api *genidentity.IAMProblem
	if errors.As(err, &api) {
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return problem("not_found")
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) {
		if pgerr.Code == "23505" || pgerr.Code == "23503" {
			return problem("conflict")
		}
		if pgerr.Code == "23514" {
			return problem("invalid")
		}
	}
	slog.Error("identity repository operation failed", "error_type", fmt.Sprintf("%T", err))
	return problem("internal")
}
func (s *Service) available() error {
	if s.repo == nil {
		return problem("unavailable")
	}
	return nil
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func tokenHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func (s *Service) cookie(value string, expired bool) string {
	return s.policyCookie(value, expired, 480)
}
func (s *Service) policyCookie(value string, expired bool, minutes int) string {
	max := minutes * 60
	if expired {
		max = -1
	}
	return (&http.Cookie{Name: CookieName, Value: value, Path: "/", HttpOnly: true, Secure: s.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: max}).String()
}
func (s *Service) APIKeyAuth(ctx context.Context, _ string, _ *security.APIKeyScheme) (context.Context, error) {
	_, err := currentLogin(ctx)
	return ctx, err
}
func (s *Service) Authenticate(ctx context.Context, raw string) (*Principal, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	if len(raw) != 64 {
		return nil, problem("unauthenticated")
	}
	session, err := s.repo.session(ctx, tokenHash(raw))
	if errors.Is(err, pgx.ErrNoRows) {
		return s.authenticateUnscopedLogin(ctx, tokenHash(raw))
	}
	if err != nil {
		return nil, safe(err)
	}
	account, err := s.repo.authorityAccount(ctx, session.TenantID, session.AccountID)
	if err != nil {
		return nil, safe(err)
	}
	account = accountForTenantContext(account)
	timing, err := s.repo.q.GovSessionTiming(ctx, tokenHash(raw))
	if err != nil {
		return nil, safe(err)
	}
	check := &Principal{Account: account, TenantID: session.TenantID}
	if err = s.sessionPolicy(ctx, check, timing.CreatedAt.Time, timing.ExpiresAt.Time, timing.LastSeenAt.Time); err != nil {
		var expired *genidentity.IAMProblem
		if errors.As(err, &expired) && expired.Code == "unauthenticated" {
			if e := s.repo.deleteSession(ctx, tokenHash(raw)); e != nil {
				return nil, safe(e)
			}
		}
		return nil, err
	}
	if err = s.repo.q.GovTouchSession(ctx, tokenHash(raw)); err != nil {
		return nil, safe(err)
	}
	p, err := s.newWorkPrincipal(ctx, account, session.TenantID, session.PrincipalID, session.LoginAccountID, session.MembershipID, session.IdentityBindingID)
	if err != nil {
		return nil, safe(err)
	}
	if err = s.loadTemporary(ctx, p); err != nil {
		return nil, err
	}
	if err = loadAuthorizationVersions(ctx, s.repo.q, p); err != nil {
		return nil, safe(err)
	}
	p.CSRF = session.CsrfToken
	p.TokenHash = tokenHash(raw)
	return p, nil
}
func CSRFMatches(p *Principal, value string) bool {
	return value != "" && subtle.ConstantTimeCompare([]byte(p.CSRF), []byte(value)) == 1
}
func profile(p *Principal) *genidentity.SessionProfile {
	if p.IsUnscopedLogin() {
		return &genidentity.SessionProfile{CsrfToken: p.CSRF, Permissions: []string{}}
	}
	version := authorizationVersion(p)
	account := &genidentity.SessionAccount{
		ID: p.Account.ID, Username: p.Account.Username, Status: p.Account.Status, IsAdmin: p.Account.IsAdmin,
	}
	result := &genidentity.SessionProfile{AuthorizationVersion: &version, Account: account, CsrfToken: p.CSRF, Permissions: p.Effective(), TenantID: p.TenantID}
	if p.IdentityBindingID != "" {
		result.IdentityBindingID = &p.IdentityBindingID
	}
	if p.ActingOrganization.OrganizationID != 0 {
		result.ActingOrganizationID = &p.ActingOrganization.OrganizationID
		result.ActingOrganizationName = &p.ActingOrganization.Name
		if p.ActingOrganization.Portal != "" {
			result.Portal = &p.ActingOrganization.Portal
		}
	}
	return result
}
func (s *Service) sessionProfile(p *Principal) *genidentity.SessionProfile {
	result := profile(p)
	if s.IsPlatformOperator(p) && p.HasTenantWideGrant("access:manage") {
		allowed := true
		result.PlatformOperator = &allowed
	}
	return result
}

func (s *Service) AuthenticatePassword(ctx context.Context, payload *genidentity.LoginPayload) (*genidentity.LoginResult, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	username := strings.ToLower(strings.TrimSpace(payload.Username))
	realm, err := applicationAuthentication(ctx)
	if err != nil {
		return nil, err
	}
	key := tokenHash(realm + ":" + username)
	login, err := s.repo.q.IAMUnscopedPasswordLogin(ctx, dbgen.IAMUnscopedPasswordLoginParams{Username: username, Realm: realm})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, safe(err)
	}
	policy := DefaultSecurityPolicy()
	throttle, throttleErr := s.repo.q.IAMLoginThrottle(ctx, key)
	if throttleErr != nil && !errors.Is(throttleErr, pgx.ErrNoRows) {
		return nil, safe(throttleErr)
	}
	if throttleErr == nil && throttle.Failures >= int32(policy.LockAttempts) && throttle.LockedUntil.Time.After(time.Now()) {
		dummyPasswordWork(payload.Password)
		if err = appendLoginDenial(ctx, s.repo.q, login.ID, "rate_limited"); err != nil {
			return nil, safe(err)
		}
		return nil, problem("unauthenticated")
	}
	valid := false
	if err == nil {
		valid = VerifyPassword(payload.Password, login.PasswordHash)
	} else {
		dummyPasswordWork(payload.Password)
	}
	if !valid {
		if err = s.repo.q.GovLoginFailure(ctx, dbgen.GovLoginFailureParams{AttemptKey: key, MaxFailures: int32(policy.LockAttempts), LockMinutes: int32(policy.LockMinutes)}); err != nil {
			return nil, safe(err)
		}
		if err = appendLoginDenial(ctx, s.repo.q, login.ID, "credentials"); err != nil {
			return nil, safe(err)
		}
		return nil, problem("unauthenticated")
	}
	return s.loginWithPassword(ctx, login, key, policy, payload)
}
func (s *Service) Me(ctx context.Context, _ *genidentity.MePayload) (*genidentity.SessionProfile, error) {
	p, err := currentLogin(ctx)
	if err != nil {
		return nil, err
	}
	return s.sessionProfile(p), nil
}
func (s *Service) Logout(ctx context.Context, _ *genidentity.LogoutPayload) (*genidentity.LogoutResult, error) {
	p, err := currentLogin(ctx)
	if err != nil {
		return nil, err
	}
	if p.IsUnscopedLogin() {
		err = s.repo.q.IAMDeleteUnscopedLoginSession(ctx, p.TokenHash)
	} else {
		err = s.repo.deleteSession(ctx, p.TokenHash)
	}
	if err != nil {
		return nil, safe(err)
	}
	return &genidentity.LogoutResult{Cookie: s.cookie("", true)}, nil
}

// ApplicationDirectoryReader supplies application data for the administration transport.
type ApplicationDirectoryReader func(context.Context, *Service, *dbgen.Queries, *Principal) ([]*genidentity.OrganizationRecord, []*genidentity.DepartmentRecord, error)

func (s *Service) ConfigureApplicationDirectoryReader(reader ApplicationDirectoryReader) {
	s.applicationDirectoryReader = reader
}

func (s *Service) Snapshot(ctx context.Context, _ *genidentity.SnapshotPayload) (*genidentity.AdminSnapshot, error) {
	p, err := Current(ctx)
	if err != nil {
		return nil, err
	}
	if p.ActingOrganization.OrganizationID != 0 {
		return nil, problem("forbidden")
	}
	out := &genidentity.AdminSnapshot{Accounts: []*genidentity.AccountRecord{}, Organizations: []*genidentity.OrganizationRecord{}, Departments: []*genidentity.DepartmentRecord{}, Roles: []*genidentity.RoleRecord{}, Permissions: s.permissionCodes(), Catalog: s.permissionCatalog()}
	if p.Can("accounts:read") {
		scope, err := s.scope(ctx, p, "accounts:read")
		if err != nil {
			return nil, err
		}
		out.Accounts, err = s.repo.accounts(ctx, scope)
		if err != nil {
			return nil, safe(err)
		}
	}
	if s.applicationDirectoryReader != nil {
		out.Organizations, out.Departments, err = s.applicationDirectoryReader(ctx, s, s.repo.q, p)
		if err != nil {
			return nil, safe(err)
		}
	}
	if p.Can("roles:read") {
		out.Roles, err = s.repo.roles(ctx, tenantScope(p))
		if err != nil {
			return nil, safe(err)
		}
	}
	return out, nil
}
func (s *Service) validateTenantGrants(grants []*genidentity.PermissionGrant) error {
	seen := map[string]bool{}
	for _, grant := range grants {
		if grant == nil || s.permissionDefinition(grant.Permission) == nil || seen[grant.Permission] ||
			grant.Effect != "allow" || (grant.Scope != "ALL" && grant.Scope != "OWN") {
			return problem("invalid")
		}
		if (grant.Permission == "access:manage" || grant.Permission == "temporary:approve") && grant.Scope != "ALL" {
			return problem("invalid")
		}
		seen[grant.Permission] = true
	}
	return nil
}
func sameJSON(left, right any) bool {
	a, err := json.Marshal(left)
	if err != nil {
		return false
	}
	b, err := json.Marshal(right)
	if err != nil {
		return false
	}
	return string(a) == string(b)
}
func (s *Service) authorized(ctx context.Context, permission string) (*Principal, Scope, error) {
	p, err := Current(ctx)
	if err != nil {
		return nil, Scope{}, err
	}
	scope, err := s.scope(ctx, p, permission)
	return p, scope, err
}
func nowString(t time.Time) string { return t.UTC().Format(time.RFC3339) }
