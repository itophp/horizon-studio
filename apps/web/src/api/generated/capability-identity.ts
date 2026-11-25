// Code generated from identity capability definitions. DO NOT EDIT.

export type IdentityAccountCreateInput = {
  email?: string;
  fullName: string;
  initialStatus: "active" | "inactive";
  password: string;
  username: string;
};
export type IdentityAccountCreateOutput = { id?: number };

export type IdentityAccountDeleteInput = { id: number };
export type IdentityAccountDeleteOutput = { id?: number };

export type IdentityAccountUpdateInput = {
  email?: string;
  fullName: string;
  id: number;
  password?: string;
  status: "active" | "inactive";
  username: string;
};
export type IdentityAccountUpdateOutput = { id?: number };

export type IdentityAccountsAdminReadInput = {};
export type IdentityAccountsAdminReadOutput = {
  catalog?: Array<Record<string, unknown>>;
  departments: Array<{
    code?: string;
    id: number;
    manager?: string;
    name: string;
    organizationId: number;
    parentId?: number;
    status: string;
  }>;
  items: Array<{
    createdAt?: string;
    departmentId?: number;
    email?: string;
    fullName: string;
    id: number;
    isAdmin?: boolean;
    memberships: Array<{
      departmentId: number;
      departmentName?: string;
      organizationId: number;
      overrides: Array<Record<string, unknown>>;
      roleIds: Array<number>;
    }>;
    organizationId?: number;
    overrides?: Array<Record<string, unknown>>;
    roleIds?: Array<number>;
    status: string;
    username: string;
  }>;
  organizations: Array<{
    code?: string;
    id: number;
    kind: string;
    name: string;
    status: string;
  }>;
  roles: Array<{
    code?: string;
    grants: Array<Record<string, unknown>>;
    id: number;
    name: string;
    status?: string;
  }>;
  total: number;
};

export type IdentityAccountsReadInput = {};
export type IdentityAccountsReadOutput = {
  items: Array<{ id: number; status: string; username: string }>;
  total: number;
};

export type IdentityAgentDelegationCreateInput = {
  additionalCapabilities?: Array<string>;
  agentAccountId: number;
  capability: string;
  maxRisk: "low" | "medium" | "high";
  minutes: number;
  password: string;
  resourceId: number;
  uses: number;
};
export type IdentityAgentDelegationCreateOutput = {
  expiresAt: string;
  id: string;
};

export type IdentityAgentDelegationListInput = { after?: string };
export type IdentityAgentDelegationListOutput = {
  items: Array<{
    additionalCapabilities: Array<string>;
    agentAccountId: number;
    capability: string;
    createdAt: string;
    expiresAt: string;
    id: string;
    maxRisk: string;
    remainingUses: number;
    resourceId: number;
    revokedAt: string;
  }>;
  nextAfter: string;
};

export type IdentityAgentDelegationRevokeInput = {
  id: string;
  password: string;
};
export type IdentityAgentDelegationRevokeOutput = { id: string };

export type IdentityAnalyticsReadInput = { timeZone?: string };
export type IdentityAnalyticsReadOutput = {
  accounts?: { active: number; total: number };
  activity: Array<{
    code: string;
    createdAt: string;
    id: number;
    key: string;
    kind: "account" | "organization" | "department" | "role";
    name: string;
  }>;
  departments?: number;
  distribution?: Array<{ label: string; value: number }>;
  organizations?: { companies: number; enterprises: number; total: number };
  recentAccounts?: Array<{
    createdAt: string;
    departments: Array<string>;
    fullName: string;
    id: number;
    status: string;
    username: string;
  }>;
  roles?: number;
  trend?: Array<{ date: string; value: number }>;
};

export type IdentityAuditConfigureExportInput = {
  factor?: string;
  password: string;
  reason: string;
  required: boolean;
};
export type IdentityAuditConfigureExportOutput = { required: boolean };

export type IdentityAuditExportInput = {
  accountId?: number;
  action?: string;
  afterId?: number;
  beforeId?: number;
  bindingId?: string;
  correlationId?: string;
  delegationId?: string;
  from?: string;
  limit?: number;
  loginAccountId?: string;
  origin?: string;
  outcome?: string;
  principalId?: string;
  requestId?: string;
  resourceId?: string;
  resourceType?: string;
  until?: string;
  workspace?: boolean;
};
export type IdentityAuditExportOutput = {
  items: Array<{
    accountId: number;
    action: string;
    at: string;
    bindingId?: string;
    capabilityVersion?: string;
    correlationId: string;
    delegationId?: string;
    id: number;
    loginAccountId?: string;
    machineCredentialId?: string;
    origin?: string;
    outcome: string;
    principalId?: string;
    reason: string;
    resourceId: string;
    resourceType: string;
  }>;
};

export type IdentityAuditExportPermitsInput = {};
export type IdentityAuditExportPermitsOutput = {
  items: Array<{
    expiresAt: string;
    id: string;
    limit: number;
    workspace: boolean;
  }>;
};

export type IdentityAuditExportPolicyInput = {};
export type IdentityAuditExportPolicyOutput = { required: boolean };

export type IdentityAuditListInput = {
  accountId?: number;
  action?: string;
  afterId?: number;
  beforeId?: number;
  bindingId?: string;
  correlationId?: string;
  delegationId?: string;
  from?: string;
  limit?: number;
  loginAccountId?: string;
  origin?: string;
  outcome?: string;
  principalId?: string;
  resourceId?: string;
  resourceType?: string;
  until?: string;
  workspace?: boolean;
};
export type IdentityAuditListOutput = {
  items: Array<{
    accountId: number;
    action: string;
    at: string;
    bindingId?: string;
    capabilityVersion?: string;
    correlationId: string;
    delegationId?: string;
    id: number;
    loginAccountId?: string;
    machineCredentialId?: string;
    origin?: string;
    outcome: string;
    principalId?: string;
    reason: string;
    resourceId: string;
    resourceType: string;
  }>;
};

export type IdentityAuditRequestExportInput = {
  filter: {
    accountId?: number;
    action?: string;
    afterId?: number;
    beforeId?: number;
    bindingId?: string;
    correlationId?: string;
    delegationId?: string;
    from?: string;
    limit?: number;
    loginAccountId?: string;
    origin?: string;
    outcome?: string;
    principalId?: string;
    resourceId?: string;
    resourceType?: string;
    until?: string;
    workspace?: boolean;
  };
  reason: string;
  requestId: string;
};
export type IdentityAuditRequestExportOutput = { id: string };

export type IdentityBindingApproveInput = { id: string; reason: string };
export type IdentityBindingApproveOutput = { id: string };

export type IdentityBindingClaimInput = {
  entityId: number;
  identityType: string;
  version: string;
};
export type IdentityBindingClaimOutput = { id: string };

export type IdentityBindingListInput = { after?: string };
export type IdentityBindingListOutput = {
  items: Array<{
    createdAt: string;
    entityId: number;
    id: string;
    identityType: string;
    identityVersion: string;
    principalId: string;
    source: string;
    status: string;
    validUntil: string;
    verificationLevel: string;
  }>;
  nextAfter: string;
};

export type IdentityBindingRecordInput = {
  entityId: number;
  identityType: string;
  version: string;
};
export type IdentityBindingRecordOutput = { id: string };

export type IdentityBindingRemoveInput = { id: string; reason: string };
export type IdentityBindingRemoveOutput = { id: string };

export type IdentityBindingRevokeInput = { id: string };
export type IdentityBindingRevokeOutput = { id: string };

export type IdentityBindingSuspendInput = { id: string; reason: string };
export type IdentityBindingSuspendOutput = { id: string };

export type IdentityContactListInput = {};
export type IdentityContactListOutput = {
  items: Array<{
    channel: "email" | "sms";
    destination: string;
    verifiedAt: string;
  }>;
};

export type IdentityContactStartInput = {
  channel: "email" | "sms";
  destination: string;
  proof: string;
};
export type IdentityContactStartOutput = { queued: boolean };

export type IdentityContactVerifyInput = { code: string };
export type IdentityContactVerifyOutput = { channel: "email" | "sms" };

export type IdentityDepartmentCreateInput = {
  code: string;
  description: string;
  manager: string;
  name: string;
  organizationId?: number;
  parentId: number;
  status: "active" | "inactive";
};
export type IdentityDepartmentCreateOutput = { id?: number };

export type IdentityDepartmentDeleteInput = { id: number };
export type IdentityDepartmentDeleteOutput = { id?: number };

export type IdentityDepartmentUpdateInput = {
  code: string;
  description: string;
  id: number;
  manager: string;
  name: string;
  organizationId: number;
  parentId: number;
  status: "active" | "inactive";
};
export type IdentityDepartmentUpdateOutput = { id?: number };

export type IdentityDepartmentsReadInput = {};
export type IdentityDepartmentsReadOutput = {
  accounts?: Array<{
    id: number;
    memberships: Array<{ departmentId: number }>;
  }>;
  items: Array<{
    code: string;
    createdAt?: string;
    description?: string;
    id: number;
    manager?: string;
    name: string;
    organizationId: number;
    parentId: number;
    status: string;
  }>;
  organizations: Array<{ id: number; kind: string; name: string }>;
  total: number;
};

export type IdentityExternalIdentityLinkInput = {
  password: string;
  proof: string;
};
export type IdentityExternalIdentityLinkOutput = {
  id?: string;
  linked: boolean;
  reviewId?: string;
  reviewRequired: boolean;
};

export type IdentityExternalIdentityListInput = {};
export type IdentityExternalIdentityListOutput = {
  items: Array<{
    id: string;
    linkedAt: string;
    providerId: string;
    revokedAt: string;
  }>;
};

export type IdentityExternalIdentityRequireExternalInput = { confirm: boolean };
export type IdentityExternalIdentityRequireExternalOutput = {
  enabled: boolean;
};

export type IdentityExternalIdentityUnlinkInput = {
  id: string;
  password: string;
};
export type IdentityExternalIdentityUnlinkOutput = { id: string };

export type IdentityExternalReviewConfirmInput = { id: string };
export type IdentityExternalReviewConfirmOutput = {
  id: string;
  status: string;
};

export type IdentityExternalReviewDecideInput = {
  decision: "apply" | "reject" | "rollback";
  id: string;
  reason: string;
};
export type IdentityExternalReviewDecideOutput = { id: string; status: string };

export type IdentityExternalReviewListInput = {
  after?: string;
  reviewer?: boolean;
};
export type IdentityExternalReviewListOutput = {
  items: Array<{
    appliedAt: string;
    createdAt: string;
    expiresAt: string;
    id: string;
    originalOwner: boolean;
    providerId: string;
    reason: string;
    requestedOwner: boolean;
    status: string;
  }>;
  nextAfter: string;
};

export type IdentityGovernanceReportInput = { windowHours?: number };
export type IdentityGovernanceReportOutput = {
  asOf: string;
  authenticationFailures: number;
  authorizationDenials: number;
  privilegedGrantTotal: number;
  privilegedGrants: Array<{
    expiresAt: string;
    id: string;
    roleName: string;
    username: string;
  }>;
  repeatedAuthentication?: Array<{
    lastAt: string;
    occurrences: number;
    username: string;
  }>;
  repeatedDenials: Array<{
    action: string;
    affectedAccounts: number;
    occurrences: number;
    reason: string;
  }>;
  windowHours: number;
};

export type IdentityLoginFreezeInput = {
  accountId: number;
  proof: string;
  reason: string;
};
export type IdentityLoginFreezeOutput = { id: number; status: string };

export type IdentityLoginReauthenticateInput = {
  accountId: number;
  operation: "freeze" | "restore";
  password: string;
};
export type IdentityLoginReauthenticateOutput = { proof: string };

export type IdentityLoginRestoreInput = {
  accountId: number;
  proof: string;
  reason: string;
};
export type IdentityLoginRestoreOutput = { id: number; status: string };

export type IdentityLoginAssociationLinkInput = {
  factor?: string;
  password: string;
  realm?: string;
  secondaryFactor?: string;
  secondaryPassword: string;
  username: string;
};
export type IdentityLoginAssociationLinkOutput = { id: string };

export type IdentityLoginAssociationListInput = { after?: string };
export type IdentityLoginAssociationListOutput = {
  items: Array<{
    createdAt: string;
    id: string;
    placed: boolean;
    realm: string;
    revokedAt: string;
    username: string;
  }>;
  nextAfter: string;
};

export type IdentityLoginAssociationUnlinkInput = {
  factor?: string;
  id: string;
  password: string;
};
export type IdentityLoginAssociationUnlinkOutput = { id: string };

export type IdentityLoginMergeConfirmInput = {
  factor?: string;
  id: string;
  password: string;
};
export type IdentityLoginMergeConfirmOutput = { id: string; status: string };

export type IdentityLoginMergeDecideInput = {
  decision: "apply" | "reject" | "rollback";
  factor?: string;
  id: string;
  password: string;
  reason: string;
};
export type IdentityLoginMergeDecideOutput = { id: string; status: string };

export type IdentityLoginMergeListInput = {
  before?: string;
  reviewer?: boolean;
};
export type IdentityLoginMergeListOutput = {
  items: Array<{
    canConfirm: boolean;
    createdAt: string;
    decisionReason: string;
    expiresAt: string;
    id: string;
    primaryUsername: string;
    reason: string;
    secondaryUsername: string;
    status: string;
  }>;
  nextBefore: string;
};

export type IdentityLoginMergeRequestInput = {
  factor?: string;
  password: string;
  realm?: string;
  reason: string;
  username: string;
};
export type IdentityLoginMergeRequestOutput = { id: string; status: string };

export type IdentityMachineCreateInput = {
  grants: Array<{ effect: "allow"; permission: string; scope: "ALL" | "OWN" }>;
  kind: "agent" | "service" | "application";
  name: string;
  password: string;
};
export type IdentityMachineCreateOutput = { id: number; principalId: string };

export type IdentityMachineCredentialListInput = {
  accountId: number;
  after?: string;
};
export type IdentityMachineCredentialListOutput = {
  items: Array<{
    audience: string;
    capabilities: Array<string>;
    createdAt: string;
    expiresAt: string;
    id: string;
    revokedAt: string;
  }>;
  nextAfter: string;
};

export type IdentityMachineCredentialRevokeInput = {
  accountId: number;
  password: string;
};
export type IdentityMachineCredentialRevokeOutput = { id?: number };

export type IdentityMachineCredentialRotateInput = {
  accountId: number;
  audience: "horizon-capability" | "horizon-agent";
  capabilities: Array<string>;
  minutes: number;
  password: string;
};
export type IdentityMachineCredentialRotateOutput = {
  expiresAt: string;
  id: string;
  secret: string;
};

export type IdentityMembershipAcceptInput = { code: string };
export type IdentityMembershipAcceptOutput = { tenantId: string };

export type IdentityMembershipInvitationRevokeInput = { id: string };
export type IdentityMembershipInvitationRevokeOutput = { id: string };

export type IdentityMembershipInviteInput = {
  realm?: string;
  username: string;
};
export type IdentityMembershipInviteOutput = {
  code: string;
  expiresAt: string;
  id: string;
};

export type IdentityMembershipListInput = { after?: number };
export type IdentityMembershipListOutput = {
  items: Array<{
    accountStatus: string;
    expiresAt: string;
    id: number;
    loginStatus: string;
    membershipStatus: string;
    principalKind: string;
    protected: boolean;
    username: string;
  }>;
  nextAfter: number;
};

export type IdentityMembershipRestoreInput = {
  accountId: number;
  expiresAt?: string;
  reason: string;
};
export type IdentityMembershipRestoreOutput = {
  expiresAt: string;
  id: number;
  status: string;
};

export type IdentityMembershipRevokeInput = { accountId: number };
export type IdentityMembershipRevokeOutput = { id?: number };

export type IdentityMembershipSetExpiryInput = {
  accountId: number;
  expiresAt?: string;
  reason: string;
};
export type IdentityMembershipSetExpiryOutput = {
  expiresAt: string;
  id: number;
  status: string;
};

export type IdentityMembershipSuspendInput = {
  accountId: number;
  reason: string;
};
export type IdentityMembershipSuspendOutput = {
  expiresAt: string;
  id: number;
  status: string;
};

export type IdentityMfaActivateInput = { code: string };
export type IdentityMfaActivateOutput = { recoveryCodes: Array<string> };

export type IdentityMfaActivateReplacementInput = { code: string };
export type IdentityMfaActivateReplacementOutput = {
  recoveryCodes: Array<string>;
};

export type IdentityMfaDisableInput = { proof: string };
export type IdentityMfaDisableOutput = {
  enabled: boolean;
  recoveryCodes: Array<string>;
};

export type IdentityMfaEnrollInput = { password: string };
export type IdentityMfaEnrollOutput = {
  expiresAt: string;
  secret: string;
  uri: string;
};

export type IdentityMfaReauthenticateInput = {
  code?: string;
  operation: "renewRecoveryCodes" | "disable" | "verifyContact" | "replace";
  password: string;
  recoveryCode?: string;
};
export type IdentityMfaReauthenticateOutput = { proof: string };

export type IdentityMfaRenewRecoveryCodesInput = { proof: string };
export type IdentityMfaRenewRecoveryCodesOutput = {
  enabled: boolean;
  recoveryCodes: Array<string>;
};

export type IdentityMfaReplaceInput = { proof: string };
export type IdentityMfaReplaceOutput = {
  expiresAt: string;
  secret: string;
  uri: string;
};

export type IdentityMfaStatusInput = {};
export type IdentityMfaStatusOutput = {
  enabled: boolean;
  pending: boolean;
  recoveryRemaining: number;
};

export type IdentityOrganizationUpdateInput = {
  code: string;
  description: string;
  id: number;
  kind: "enterprise" | "company";
  name: string;
  parentId: number;
  status: "active" | "inactive";
};
export type IdentityOrganizationUpdateOutput = { id?: number };

export type IdentityOrganizationsReadInput = {};
export type IdentityOrganizationsReadOutput = {
  items: Array<{
    code: string;
    createdAt?: string;
    description?: string;
    id: number;
    kind: string;
    name: string;
    parentId: number;
    status: string;
  }>;
  total: number;
};

export type IdentityPolicyCheckInput = {
  rules: Array<{
    action: string;
    attribute?: string;
    bindingType?: string;
    effect:
      | "deny"
      | "require_binding"
      | "require_attribute"
      | "filter_row"
      | "mask_output";
    equals?: string;
    resource: string;
  }>;
};
export type IdentityPolicyCheckOutput = {
  issues: Array<{ code: string; rule: number }>;
};

export type IdentityPolicyDraftInput = {
  baseVersion: number;
  reason: string;
  rules: Array<{
    action: string;
    attribute?: string;
    bindingType?: string;
    effect:
      | "deny"
      | "require_binding"
      | "require_attribute"
      | "filter_row"
      | "mask_output";
    equals?: string;
    resource: string;
  }>;
};
export type IdentityPolicyDraftOutput = { id: number };

export type IdentityPolicyHistoryInput = {};
export type IdentityPolicyHistoryOutput = {
  items: Array<{
    authorId: number;
    authoredByMe: boolean;
    baseVersion: number;
    id: number;
    published: boolean;
    reason: string;
  }>;
  version: number;
};

export type IdentityPolicyPreviewInput = { id: number };
export type IdentityPolicyPreviewOutput = {
  additions: Array<{
    action: string;
    attribute?: string;
    bindingType?: string;
    effect:
      | "deny"
      | "require_binding"
      | "require_attribute"
      | "filter_row"
      | "mask_output";
    equals?: string;
    resource: string;
  }>;
  authorName?: string;
  authoredByMe: boolean;
  baseVersion: number;
  createdAt?: string;
  currentVersion: number;
  issues?: Array<{ code: string; rule: number }>;
  proposedRules: Array<{
    action: string;
    attribute?: string;
    bindingType?: string;
    effect:
      | "deny"
      | "require_binding"
      | "require_attribute"
      | "filter_row"
      | "mask_output";
    equals?: string;
    resource: string;
  }>;
  proposedVersion: number;
  published: boolean;
  publishedAt?: string;
  reason: string;
  removals: Array<{
    action: string;
    attribute?: string;
    bindingType?: string;
    effect:
      | "deny"
      | "require_binding"
      | "require_attribute"
      | "filter_row"
      | "mask_output";
    equals?: string;
    resource: string;
  }>;
  reviewerName?: string;
  stale: boolean;
};

export type IdentityPolicyPublishInput = { id: number };
export type IdentityPolicyPublishOutput = { id: number };

export type IdentityPolicyRollbackInput = {
  baseVersion: number;
  id: number;
  reason: string;
};
export type IdentityPolicyRollbackOutput = { id: number };

export type IdentityPolicySearchContextsInput = { keyword: string };
export type IdentityPolicySearchContextsOutput = {
  items: Array<{ accountId: number; fullName: string; username: string }>;
};

export type IdentityPolicySimulateInput = {
  accountId: number;
  action: string;
  bindingId?: string;
  capabilityVersion?: string;
  id: number;
  permission?: string;
  resource: string;
  resourceId?: number;
};
export type IdentityPolicySimulateOutput = {
  currentAllowed: boolean;
  currentConstraints?: {
    maskedFields: Array<string>;
    requiresBinding: boolean;
    rowPredicates: Array<{ attribute: string; equals: string }>;
  };
  currentReason: string;
  currentVersion: number;
  proposedAllowed: boolean;
  proposedConstraints?: {
    maskedFields: Array<string>;
    requiresBinding: boolean;
    rowPredicates: Array<{ attribute: string; equals: string }>;
  };
  proposedReason: string;
  proposedVersion: number;
};

export type IdentityPrincipalCreateInput = {
  displayName: string;
  initialPassword: string;
  proof: string;
  realm?: string;
  reason: string;
  target: string;
  targetTenantId: string;
};
export type IdentityPrincipalCreateOutput = { id: string };

export type IdentityPrincipalDetailInput = {
  principalId: string;
  reason: string;
  targetTenantId: string;
};
export type IdentityPrincipalDetailOutput = {
  auditEvents: Array<{
    at: string;
    context: string;
    id: string;
    label: string;
    status: string;
  }>;
  bindings: Array<{
    at: string;
    context: string;
    id: string;
    label: string;
    status: string;
  }>;
  createdAt: string;
  displayName: string;
  id: string;
  kind: string;
  logins: Array<{
    at: string;
    context: string;
    id: string;
    label: string;
    status: string;
  }>;
  memberships: Array<{
    at: string;
    context: string;
    id: string;
    label: string;
    status: string;
  }>;
  riskLevel: "none" | "elevated" | "high";
  roles: Array<{
    at: string;
    context: string;
    id: string;
    label: string;
    status: string;
  }>;
  securityNotes: string;
  sessions: Array<{
    at: string;
    context: string;
    id: string;
    label: string;
    status: string;
  }>;
  source: string;
  status: string;
};

export type IdentityPrincipalDisableInput = {
  proof: string;
  reason: string;
  target: string;
  targetTenantId: string;
};
export type IdentityPrincipalDisableOutput = { id: string };

export type IdentityPrincipalListInput = {
  after?: string;
  principalId?: string;
  targetTenantId: string;
};
export type IdentityPrincipalListOutput = {
  items: Array<{
    createdAt: string;
    displayName: string;
    id: string;
    kind: string;
    loginCount: number;
    riskLevel: "none" | "elevated" | "high";
    source: string;
    status: string;
    workspaceCount: number;
  }>;
  nextAfter: string;
};

export type IdentityPrincipalReauthenticateInput = {
  factor?: string;
  operation:
    | "create"
    | "suspend"
    | "disable"
    | "restore"
    | "tenant.create"
    | "tenant.deactivate"
    | "tenant.restore"
    | "setSecurity";
  password: string;
  reason: string;
  riskLevel?: "none" | "elevated" | "high";
  securityNotes?: string;
  target: string;
  targetTenantId: string;
};
export type IdentityPrincipalReauthenticateOutput = { proof: string };

export type IdentityPrincipalRestoreInput = {
  proof: string;
  reason: string;
  target: string;
  targetTenantId: string;
};
export type IdentityPrincipalRestoreOutput = { id: string };

export type IdentityPrincipalSetSecurityInput = {
  proof: string;
  reason: string;
  riskLevel: "none" | "elevated" | "high";
  securityNotes: string;
  target: string;
  targetTenantId: string;
};
export type IdentityPrincipalSetSecurityOutput = { id: string };

export type IdentityPrincipalSuspendInput = {
  proof: string;
  reason: string;
  target: string;
  targetTenantId: string;
};
export type IdentityPrincipalSuspendOutput = { id: string };

export type IdentityResourceListInput = { after?: string; search?: string };
export type IdentityResourceListOutput = {
  items: Array<{
    actions: Array<{
      attributeProvider: boolean;
      machineCallable: boolean;
      maskedFields: Array<string>;
      module: string;
      name: string;
      origins: Array<string>;
      permission: string;
      resourceSelector: string;
      rowAttributes: Array<string>;
      version: string;
    }>;
    canDisable: boolean;
    enabled: boolean;
    resource: string;
  }>;
  nextAfter: string;
};

export type IdentityResourceSetStatusInput = {
  enabled: boolean;
  factor?: string;
  password: string;
  reason: string;
  resource: string;
};
export type IdentityResourceSetStatusOutput = {
  enabled: boolean;
  resource: string;
};

export type IdentityReviewConfigureInput = { intervalDays: number };
export type IdentityReviewConfigureOutput = { nextAt: string };

export type IdentityReviewDecideInput = {
  decision: "retain" | "revoke";
  id: number;
  reason: string;
};
export type IdentityReviewDecideOutput = { id: number; status: string };

export type IdentityReviewListInput = {
  after?: number;
  status?: "pending" | "retained" | "revoked" | "superseded";
};
export type IdentityReviewListOutput = {
  items: Array<{
    createdAt: string;
    id: number;
    kind: "membership" | "context_role";
    principalId: string;
    principalName?: string;
    reason?: string;
    reviewedAt?: string;
    reviewerId?: number;
    roleId?: number;
    roleName?: string;
    status: string;
    targetExpiresAt?: string;
    targetId: string;
    username?: string;
  }>;
  nextAfter: number;
};

export type IdentityReviewRunDueInput = { expectedDueAt: string };
export type IdentityReviewRunDueOutput = { created: number };

export type IdentityReviewStartInput = {};
export type IdentityReviewStartOutput = { created: number };

export type IdentityRoleCopyInput = {
  code: string;
  name: string;
  sourceId: number;
};
export type IdentityRoleCopyOutput = { id?: number };

export type IdentityRoleCreateInput = {
  code: string;
  description: string;
  grants: Array<{ effect: "allow"; permission: string; scope: "ALL" | "OWN" }>;
  name: string;
  status: "active" | "inactive";
};
export type IdentityRoleCreateOutput = { id?: number };

export type IdentityRoleDeleteInput = { id: number };
export type IdentityRoleDeleteOutput = { id?: number };

export type IdentityRolePreviewInput = {
  code: string;
  description: string;
  grants: Array<{ effect: "allow"; permission: string; scope: "ALL" | "OWN" }>;
  id: number;
  name: string;
  status: "active" | "inactive";
};
export type IdentityRolePreviewOutput = {
  added: Array<{ effect: "allow"; permission: string; scope: "ALL" | "OWN" }>;
  impact: { principals: number; sessions: number; tasks: number };
  removed: Array<{ effect: "allow"; permission: string; scope: "ALL" | "OWN" }>;
  revision: string;
  statusAfter: "active" | "inactive";
  statusBefore: "active" | "inactive";
};

export type IdentityRoleUpdateInput = {
  code: string;
  description: string;
  grants: Array<{ effect: "allow"; permission: string; scope: "ALL" | "OWN" }>;
  id: number;
  name: string;
  revision?: string;
  status: "active" | "inactive";
};
export type IdentityRoleUpdateOutput = { id?: number };

export type IdentityRoleGrantCreateInput = {
  accountId: number;
  bindingId?: string;
  minutes: number;
  reason: string;
  roleId: number;
};
export type IdentityRoleGrantCreateOutput = { id: string };

export type IdentityRoleGrantListInput = {};
export type IdentityRoleGrantListOutput = {
  items: Array<{
    bindingId?: string;
    createdAt: string;
    expiresAt: string;
    grantedBy: number;
    id: string;
    principalId: string;
    reason?: string;
    revokedAt?: string;
    roleId: number;
  }>;
};

export type IdentityRoleGrantPreviewRevokeInput = { id: string };
export type IdentityRoleGrantPreviewRevokeOutput = {
  principals: number;
  sessions: number;
  tasks: number;
};

export type IdentityRoleGrantRevokeInput = { id: string };
export type IdentityRoleGrantRevokeOutput = { id: string };

export type IdentityRolesReadInput = {};
export type IdentityRolesReadOutput = {
  catalog: Array<Record<string, unknown>>;
  items: Array<{
    code: string;
    createdAt?: string;
    description?: string;
    grants: Array<Record<string, unknown>>;
    id: number;
    memberCount?: number;
    name: string;
    status: string;
  }>;
  total: number;
};

export type IdentitySessionListInput = {};
export type IdentitySessionListOutput = {
  items: Array<{
    createdAt: string;
    current: boolean;
    device: string;
    expiresAt: string;
    id: string;
    lastSeenAt: string;
    tenantId: string;
  }>;
};

export type IdentitySessionReauthenticateInput = {
  id: string;
  password: string;
};
export type IdentitySessionReauthenticateOutput = { proof: string };

export type IdentitySessionRevokeInput = { id: string; proof: string };
export type IdentitySessionRevokeOutput = { id: string };

export type IdentityTenantCreateInput = {
  initialPassword: string;
  proof: string;
  reason: string;
  target: string;
  targetTenantId: string;
};
export type IdentityTenantCreateOutput = { id: string };

export type IdentityTenantDeactivateInput = {
  proof: string;
  reason: string;
  targetTenantId: string;
};
export type IdentityTenantDeactivateOutput = { id: string };

export type IdentityTenantListInput = { after?: string };
export type IdentityTenantListOutput = {
  items: Array<{ createdAt: string; id: string; status: string }>;
  nextAfter: string;
};

export type IdentityTenantRegistrationConfigureInput = {
  channel: "invite" | "email" | "sms";
};
export type IdentityTenantRegistrationConfigureOutput = {
  channel: string;
  id: string;
};

export type IdentityTenantRegistrationReadInput = {};
export type IdentityTenantRegistrationReadOutput = {
  channel: "invite" | "email" | "sms";
};

export type IdentityTenantRestoreInput = {
  proof: string;
  reason: string;
  targetTenantId: string;
};
export type IdentityTenantRestoreOutput = { id: string };
