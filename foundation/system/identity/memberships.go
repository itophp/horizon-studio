package identity

import genidentity "horizon/foundation/gen/identity"

func accountForTenantContext(account *genidentity.AccountRecord) *genidentity.AccountRecord {
	placement := *account
	placement.DepartmentID = 0
	placement.OrganizationID = 0
	placement.Memberships = []*genidentity.DepartmentMembership{}
	return &placement
}
