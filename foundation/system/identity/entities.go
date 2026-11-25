package identity

import "horizon/foundation/kernel/capability/contracts"

// Entities lists the identity objects that have a model.
func Entities() []contracts.Entity {
	return []contracts.Entity{
		accountEntity(),
		securityPrincipalEntity(),
		loginAccountEntity(),
		loginAssociationEntityModel(),
		loginMergeEntityModel(),
		loginSessionEntity(),
		verifiedContactEntity(),
		contactChallengeEntity(),
		pendingRegistrationEntity(),
		passwordCredentialEntity(),
		machineCredentialEntity(),
		externalIdentityEntity(),
		externalIdentityReviewEntity(),
		externalIdentityProofEntity(),
		federationFlowEntity(),
		federationAssertionUseEntity(),
		agentDelegationEntity(),
		tenantEntity(),
		accessReviewEntity(),
		tenantMembershipEntity(),
		membershipInvitationEntity(),
		identityBindingEntity(),
		sessionEntity(),
		roleEntity(),
		accountRoleEntity(),
		contextRoleGrantEntity(),
		policyVersionEntity(),
		policyHeadEntity(),
		securityAuditEntity(),
		resourceStateEntity(),
		auditExportPermitModel(),
		securityCheckpointEntity(),
		securityRetentionReceiptEntity(),
		totpCredentialEntity(),
		recoveryCodeEntity(),
	}
}
