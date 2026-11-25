package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"horizon/internal/repository/dbgen"
	"sort"
)

// authorizationVersion contains no CSRF nonce, credential, name or policy text.
// The hash changes when effective grants/scopes change even if permission codes do not.
func authorizationVersion(p *Principal) string {
	parts := []string{fmt.Sprintf("principal:%q:%q:%q:%q:%q:%d:%t:%d", p.SubjectID, p.LoginAccountID, p.MembershipID, p.IdentityBindingID, p.TenantID, p.Account.ID, p.Account.IsAdmin, p.PolicyVersion), fmt.Sprint("resources:", p.ResourceRevision)}
	parts = append(parts, fmt.Sprintf("acting:%d:%q", p.ActingOrganization.OrganizationID, p.ActingOrganization.Portal))
	for _, permission := range p.Effective() {
		parts = append(parts, fmt.Sprintf("permission:%q", permission))
	}
	for _, grant := range p.Grants {
		parts = append(parts, fmt.Sprintf("grant:%q:%q:%q", grant.Permission, grant.Effect, grant.Scope))
	}
	for permission, scopes := range p.TemporaryScopes {
		for _, scope := range scopes {
			parts = append(parts, fmt.Sprintf("temporary:%q:%q:%t:%d", permission, scope.TenantID, scope.All, scope.OwnID))
		}
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(fmt.Sprint(parts)))
	return hex.EncodeToString(sum[:])
}

// AuthorizationVersion is a non-sensitive freshness stamp, not a permission grant.
func AuthorizationVersion(p *Principal) string { return authorizationVersion(p) }

func loadAuthorizationVersions(ctx context.Context, queries *dbgen.Queries, principal *Principal) error {
	row, err := queries.IAMAuthorizationVersions(ctx, principal.TenantID)
	if err == nil {
		principal.PolicyVersion, principal.ResourceRevision = row.PolicyVersion, row.ResourceRevision
	}
	return safe(err)
}
