// Capability SDK for the console. Every write and controlled read goes through
// the gateway: the functions below only shape arguments and unwrap the uniform
// result, so a page button and an agent tool run the same code path.
import { capabilityInvoke } from "@/api/generated/baseline";
import { ApiError } from "@/api/client/request";
import type {
  IdentityAuditListInput,
  IdentityAuditListOutput,
  IdentityAuditExportInput,
  IdentityAuditExportOutput,
  IdentityExternalIdentityLinkInput,
  IdentityExternalIdentityLinkOutput,
  IdentityExternalIdentityListOutput,
  IdentityExternalIdentityUnlinkInput,
  IdentityExternalIdentityUnlinkOutput,
  IdentitySessionListOutput,
  IdentitySessionReauthenticateInput,
  IdentitySessionReauthenticateOutput,
  IdentitySessionRevokeInput,
  IdentitySessionRevokeOutput,
  IdentityMfaStatusOutput,
  IdentityMfaEnrollInput,
  IdentityMfaEnrollOutput,
  IdentityMfaActivateInput,
  IdentityMfaActivateOutput,
} from "@/api/generated/capability-identity";
import type * as EP from "@/api/generated/capability-execution-plans";
import type * as IAM from "@/api/generated/capability-identity";
import type {
  KnowledgeQueryInput,
  KnowledgeQueryOutput,
  KnowledgeReadInput,
  KnowledgeReadOutput,
  KnowledgeUpdateInput,
  KnowledgeUpdateOutput,
} from "@/api/generated/capability-knowledge";
import type {
  ChangeEntry,
  ChangeRecord,
  DictionaryDetail,
  DictionaryOption,
  DeliveryRecord,
  ImportPreview,
  InboxMessage,
  MCPTokenCreated,
  ManagedFile,
  NotificationAudience,
  SentNotification,
  SettingSchedule,
  SettingsWorkspace,
  StorageDriver,
  ReauthProof,
} from "@/api/types";
import type {
  CapabilityResult,
  EffectiveConfiguration,
  FileContent,
} from "@/api/generated/models";
import type {
  MemoryActivityCollectInput,
  MemoryActivityCollectOutput,
  MemoryEventListInput,
  MemoryEventListOutput,
  MemoryIngestionFailuresOutput,
  MemoryIngestionReplayOutput,
  MemoryIngestionReplayRangeInput,
  MemoryIngestionReplayRangeOutput,
  MemoryItemCreateInput,
  MemoryItemCreateOutput,
  MemoryItemGetOutput,
  MemoryItemListInput,
  MemoryItemListOutput,
  MemoryItemCorrectInput,
  MemoryItemCorrectOutput,
  MemoryItemArchiveOutput,
  MemoryItemDeleteOutput,
  MemoryItemSourcesOutput,
  MemoryProjectCreateInput,
  MemoryProjectCreateOutput,
  MemoryProjectListInput,
  MemoryProjectListOutput,
  MemoryProjectEventsInput,
  MemoryProjectEventsOutput,
  MemoryProjectGetOutput,
  MemoryProjectSourcesOutput,
  MemoryProjectCorrectInput,
  MemoryProjectCorrectOutput,
  MemoryProjectArchiveOutput,
  MemoryProjectDeleteOutput,
  MemoryPolicyListOutput,
  MemoryPolicySetInput,
  MemoryPolicySetOutput,
  MemoryGroupCreateInput,
  MemoryGroupCreateOutput,
  MemoryGroupListInput,
  MemoryGroupListOutput,
  MemoryGroupGetInput,
  MemoryGroupGetOutput,
  MemoryGroupSourcesOutput,
  MemoryGroupSourcesInput,
  MemoryGroupCorrectInput,
  MemoryGroupCorrectOutput,
  MemoryGroupArchiveInput,
  MemoryGroupArchiveOutput,
  MemoryGroupDeleteOutput,
  MemoryGroupDeleteInput,
  MemoryEntityCreateInput,
  MemoryEntityCreateOutput,
  MemoryEntityListInput,
  MemoryEntityListOutput,
  MemoryEntityGetInput,
  MemoryEntityGetOutput,
  MemoryEntitySourcesOutput,
  MemoryEntitySourcesInput,
  MemoryEntityCorrectInput,
  MemoryEntityCorrectOutput,
  MemoryEntityArchiveInput,
  MemoryEntityArchiveOutput,
  MemoryEntityDeleteOutput,
  MemoryEntityDeleteInput,
} from "@/api/generated/capability-memory";

export type KnowledgeDocument = {
  id: number;
  title: string;
  source: string;
  chunks: number;
  createdAt: string;
};

export type CapabilityResponse<T> = {
  status: number;
  data: T;
  headers: Headers;
};

const emptyHeaders = new Headers();

export const identityMembershipAccept = (
  input: IAM.IdentityMembershipAcceptInput,
  key: string,
) =>
  invoke<IAM.IdentityMembershipAcceptOutput>(
    "identity.membership.accept",
    input,
    key,
  );

export const identityMembershipInvite = (
  input: IAM.IdentityMembershipInviteInput,
  key: string,
) =>
  invoke<IAM.IdentityMembershipInviteOutput>(
    "identity.membership.invite",
    input,
    key,
  );

export const identityMembershipList = (
  input: IAM.IdentityMembershipListInput,
  signal?: AbortSignal,
) =>
  invoke<IAM.IdentityMembershipListOutput>(
    "identity.membership.list",
    input,
    undefined,
    signal,
  );

export const identityContactList = (signal?: AbortSignal) =>
  invoke<IAM.IdentityContactListOutput>(
    "identity.contact.list",
    {},
    undefined,
    signal,
  );
export const identityContactStart = (
  input: IAM.IdentityContactStartInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityContactStartOutput>(
    "identity.contact.start",
    input,
    key,
    signal,
  );
export const identityContactVerify = (
  input: IAM.IdentityContactVerifyInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityContactVerifyOutput>(
    "identity.contact.verify",
    input,
    key,
    signal,
  );
export const identityMfaReauthenticate = (
  input: IAM.IdentityMfaReauthenticateInput,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityMfaReauthenticateOutput>(
    "identity.mfa.reauthenticate",
    input,
    undefined,
    signal,
  );
export const identityMfaRenewRecoveryCodes = (
  input: IAM.IdentityMfaRenewRecoveryCodesInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityMfaRenewRecoveryCodesOutput>(
    "identity.mfa.renewRecoveryCodes",
    input,
    key,
    signal,
  );
export const identityMfaDisable = (
  input: IAM.IdentityMfaDisableInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityMfaDisableOutput>(
    "identity.mfa.disable",
    input,
    key,
    signal,
  );

export const identityMfaReplace = (
  input: IAM.IdentityMfaReplaceInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityMfaReplaceOutput>(
    "identity.mfa.replace",
    input,
    key,
    signal,
  );
export const identityMfaActivateReplacement = (
  input: IAM.IdentityMfaActivateReplacementInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityMfaActivateReplacementOutput>(
    "identity.mfa.activateReplacement",
    input,
    key,
    signal,
  );

export const identityLoginReauthenticate = (
  input: IAM.IdentityLoginReauthenticateInput,
  key: string,
  signal?: AbortSignal,
) =>
  invoke<IAM.IdentityLoginReauthenticateOutput>(
    "identity.login.reauthenticate",
    input,
    key,
    signal,
  );
export const identityLoginFreeze = (
  input: IAM.IdentityLoginFreezeInput,
  key: string,
  signal?: AbortSignal,
) =>
  invoke<IAM.IdentityLoginFreezeOutput>(
    "identity.login.freeze",
    input,
    key,
    signal,
  );
export const identityLoginRestore = (
  input: IAM.IdentityLoginRestoreInput,
  key: string,
  signal?: AbortSignal,
) =>
  invoke<IAM.IdentityLoginRestoreOutput>(
    "identity.login.restore",
    input,
    key,
    signal,
  );
export const identityMembershipRevoke = (
  input: IAM.IdentityMembershipRevokeInput,
  key: string,
  signal?: AbortSignal,
) =>
  invoke<IAM.IdentityMembershipRevokeOutput>(
    "identity.membership.revoke",
    input,
    key,
    signal,
  );

export const identityPlatformWorkspaces = (
  after: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityTenantListOutput>(
    "identity.tenant.list",
    after ? { after } : {},
    undefined,
    signal,
  );
export const identityPlatformPrincipals = (
  targetTenantId: string,
  after: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityPrincipalListOutput>(
    "identity.principal.list",
    { targetTenantId, ...(after && { after }) },
    undefined,
    signal,
  );
export const identityPlatformProof = (
  input: IAM.IdentityPrincipalReauthenticateInput,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityPrincipalReauthenticateOutput>(
    "identity.principal.reauthenticate",
    input,
    undefined,
    signal,
  );
export const identityPrincipalDetail = (
  input: IAM.IdentityPrincipalDetailInput,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityPrincipalDetailOutput>(
    "identity.principal.detail",
    input,
    undefined,
    signal,
  );
export const identityPrincipalSetSecurity = (
  input: IAM.IdentityPrincipalSetSecurityInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityPrincipalSetSecurityOutput>(
    "identity.principal.setSecurity",
    input,
    key,
    signal,
  );
export const identityLoginMergeList = (
  input: IAM.IdentityLoginMergeListInput,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityLoginMergeListOutput>(
    "identity.loginMerge.list",
    input,
    undefined,
    signal,
  );
export const identityLoginMergeRequest = (
  input: IAM.IdentityLoginMergeRequestInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityLoginMergeRequestOutput>(
    "identity.loginMerge.request",
    input,
    key,
    signal,
  );
export const identityLoginMergeConfirm = (
  input: IAM.IdentityLoginMergeConfirmInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityLoginMergeConfirmOutput>(
    "identity.loginMerge.confirm",
    input,
    key,
    signal,
  );
export const identityLoginMergeDecide = (
  input: IAM.IdentityLoginMergeDecideInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityLoginMergeDecideOutput>(
    "identity.loginMerge.decide",
    input,
    key,
    signal,
  );
export function identityPlatformChange(
  operation: IAM.IdentityPrincipalReauthenticateInput["operation"],
  input: IAM.IdentityPrincipalCreateInput,
  key: string,
  signal: AbortSignal,
) {
  const {
    targetTenantId,
    reason,
    proof,
    target,
    initialPassword,
    displayName,
  } = input;
  const common = { targetTenantId, reason, proof };
  switch (operation) {
    case "tenant.create":
      return invoke<IAM.IdentityTenantCreateOutput>(
        "identity.tenant.create",
        { ...common, target, initialPassword },
        key,
        signal,
      );
    case "tenant.deactivate":
    case "tenant.restore":
      return invoke<IAM.IdentityTenantRestoreOutput>(
        "identity." + operation,
        common,
        key,
        signal,
      );
    case "create":
      return invoke<IAM.IdentityPrincipalCreateOutput>(
        "identity.principal.create",
        { ...common, target, displayName, initialPassword },
        key,
        signal,
      );
    default:
      return invoke<IAM.IdentityPrincipalRestoreOutput>(
        "identity.principal." + operation,
        { ...common, target },
        key,
        signal,
      );
  }
}

export const identityRegistrationPolicy = (signal?: AbortSignal) =>
  invoke<IAM.IdentityTenantRegistrationReadOutput>(
    "identity.tenant.registration.read",
    {},
    undefined,
    signal,
  );
export const identityConfigureRegistration = (
  input: IAM.IdentityTenantRegistrationConfigureInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityTenantRegistrationConfigureOutput>(
    "identity.tenant.registration.configure",
    input,
    key,
    signal,
  );

export const identityReviewList = (
  input: IAM.IdentityReviewListInput,
  signal?: AbortSignal,
) =>
  invoke<IAM.IdentityReviewListOutput>(
    "identity.review.list",
    input,
    undefined,
    signal,
  );
export const identityExternalReviewList = (
  input: IAM.IdentityExternalReviewListInput,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityExternalReviewListOutput>(
    "identity.externalReview.list",
    input,
    undefined,
    signal,
  );
export const machineCredentialList = (
  input: IAM.IdentityMachineCredentialListInput,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityMachineCredentialListOutput>(
    "identity.machine.credential.list",
    input,
    undefined,
    signal,
  );
export const machineCredentialRotate = (
  input: IAM.IdentityMachineCredentialRotateInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityMachineCredentialRotateOutput>(
    "identity.machine.credential.rotate",
    input,
    key,
    signal,
  );
export const machineCredentialRevoke = (
  input: IAM.IdentityMachineCredentialRevokeInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityMachineCredentialRevokeOutput>(
    "identity.machine.credential.revoke",
    input,
    key,
    signal,
  );
export const agentDelegationList = (
  input: IAM.IdentityAgentDelegationListInput,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityAgentDelegationListOutput>(
    "identity.agentDelegation.list",
    input,
    undefined,
    signal,
  );
export const agentDelegationRevoke = (
  input: IAM.IdentityAgentDelegationRevokeInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityAgentDelegationRevokeOutput>(
    "identity.agentDelegation.revoke",
    input,
    key,
    signal,
  );
export const loginAssociationList = (
  input: IAM.IdentityLoginAssociationListInput,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityLoginAssociationListOutput>(
    "identity.loginAssociation.list",
    input,
    undefined,
    signal,
  );
export const loginAssociationLink = (
  input: IAM.IdentityLoginAssociationLinkInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityLoginAssociationLinkOutput>(
    "identity.loginAssociation.link",
    input,
    key,
    signal,
  );
export const loginAssociationUnlink = (
  input: IAM.IdentityLoginAssociationUnlinkInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityLoginAssociationUnlinkOutput>(
    "identity.loginAssociation.unlink",
    input,
    key,
    signal,
  );
export const identityBindingList = (
  input: IAM.IdentityBindingListInput,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityBindingListOutput>(
    "identity.binding.list",
    input,
    undefined,
    signal,
  );
export const identityBindingChange = (
  action: "approve" | "suspend" | "remove",
  input: IAM.IdentityBindingApproveInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityBindingApproveOutput>(
    `identity.binding.${action}`,
    input,
    key,
    signal,
  );
export const identityExternalReviewConfirm = (
  input: IAM.IdentityExternalReviewConfirmInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityExternalReviewConfirmOutput>(
    "identity.externalReview.confirm",
    input,
    key,
    signal,
  );
export const identityExternalReviewDecide = (
  input: IAM.IdentityExternalReviewDecideInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityExternalReviewDecideOutput>(
    "identity.externalReview.decide",
    input,
    key,
    signal,
  );
export const identityReviewStart = (key: string, signal: AbortSignal) =>
  invoke<IAM.IdentityReviewStartOutput>(
    "identity.review.start",
    {},
    key,
    signal,
  );
export const identityReviewDecide = (
  input: IAM.IdentityReviewDecideInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityReviewDecideOutput>(
    "identity.review.decide",
    input,
    key,
    signal,
  );
export const identityReviewConfigure = (
  input: IAM.IdentityReviewConfigureInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityReviewConfigureOutput>(
    "identity.review.configure",
    input,
    key,
    signal,
  );

export const identityMembershipSuspend = (
  input: IAM.IdentityMembershipSuspendInput,
  key: string,
  signal?: AbortSignal,
) =>
  invoke<IAM.IdentityMembershipSuspendOutput>(
    "identity.membership.suspend",
    input,
    key,
    signal,
  );

export const identityMembershipRestore = (
  input: IAM.IdentityMembershipRestoreInput,
  key: string,
  signal?: AbortSignal,
) =>
  invoke<IAM.IdentityMembershipRestoreOutput>(
    "identity.membership.restore",
    input,
    key,
    signal,
  );

export const identityMembershipSetExpiry = (
  input: IAM.IdentityMembershipSetExpiryInput,
  key: string,
  signal?: AbortSignal,
) =>
  invoke<IAM.IdentityMembershipSetExpiryOutput>(
    "identity.membership.setExpiry",
    input,
    key,
    signal,
  );

export const identityGovernanceReport = (
  input: IAM.IdentityGovernanceReportInput,
  signal?: AbortSignal,
) =>
  invoke<IAM.IdentityGovernanceReportOutput>(
    "identity.governance.report",
    input,
    undefined,
    signal,
  );

export const identityPolicyHistory = (signal?: AbortSignal) =>
  invoke<IAM.IdentityPolicyHistoryOutput>(
    "identity.policy.history",
    {},
    undefined,
    signal,
  );
export const identityPolicyPreview = (
  input: IAM.IdentityPolicyPreviewInput,
  signal?: AbortSignal,
) =>
  invoke<IAM.IdentityPolicyPreviewOutput>(
    "identity.policy.preview",
    input,
    undefined,
    signal,
  );
export const identityPolicyDraft = (
  input: IAM.IdentityPolicyDraftInput,
  key: string,
) => invoke<IAM.IdentityPolicyDraftOutput>("identity.policy.draft", input, key);
export const identityPolicyCheck = (
  input: IAM.IdentityPolicyCheckInput,
  signal?: AbortSignal,
) =>
  invoke<IAM.IdentityPolicyCheckOutput>(
    "identity.policy.check",
    input,
    undefined,
    signal,
  );
export const identityPolicySearchContexts = (
  input: IAM.IdentityPolicySearchContextsInput,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityPolicySearchContextsOutput>(
    "identity.policy.searchContexts",
    input,
    undefined,
    signal,
  );
export const identityPolicyRollback = (
  input: IAM.IdentityPolicyRollbackInput,
  key: string,
) =>
  invoke<IAM.IdentityPolicyRollbackOutput>(
    "identity.policy.rollback",
    input,
    key,
  );
export const identityPolicyPublish = (
  input: IAM.IdentityPolicyPublishInput,
  key: string,
) =>
  invoke<IAM.IdentityPolicyPublishOutput>(
    "identity.policy.publish",
    input,
    key,
  );
export const identityPolicySimulate = (
  input: IAM.IdentityPolicySimulateInput,
  signal?: AbortSignal,
) =>
  invoke<IAM.IdentityPolicySimulateOutput>(
    "identity.policy.simulate",
    input,
    undefined,
    signal,
  );

export const securityAuditList = (
  input: IdentityAuditListInput,
  signal?: AbortSignal,
) =>
  invoke<IdentityAuditListOutput>(
    "identity.audit.list",
    input,
    undefined,
    signal,
  );
export const securityAuditExport = (
  input: IdentityAuditExportInput,
  signal?: AbortSignal,
) =>
  invoke<IdentityAuditExportOutput>(
    "identity.audit.export",
    input,
    undefined,
    signal,
  );

export const externalIdentityLink = (
  input: IdentityExternalIdentityLinkInput,
  key: string,
) =>
  invoke<IdentityExternalIdentityLinkOutput>(
    "identity.externalIdentity.link",
    input,
    key,
  );

export const externalIdentityList = () =>
  invoke<IdentityExternalIdentityListOutput>("identity.externalIdentity.list");
export const externalIdentityUnlink = (
  input: IdentityExternalIdentityUnlinkInput,
  key: string,
  signal?: AbortSignal,
) =>
  invoke<IdentityExternalIdentityUnlinkOutput>(
    "identity.externalIdentity.unlink",
    input,
    key,
    signal,
  );
export const identitySessions = () =>
  invoke<IdentitySessionListOutput>("identity.session.list");
export const identitySessionReauthenticate = (
  input: IdentitySessionReauthenticateInput,
  key: string,
  signal?: AbortSignal,
) =>
  invoke<IdentitySessionReauthenticateOutput>(
    "identity.session.reauthenticate",
    input,
    key,
    signal,
  );
export const identitySessionRevoke = (
  input: IdentitySessionRevokeInput,
  key: string,
  signal?: AbortSignal,
) =>
  invoke<IdentitySessionRevokeOutput>(
    "identity.session.revoke",
    input,
    key,
    signal,
  );
export const identityMfaStatus = () =>
  invoke<IdentityMfaStatusOutput>("identity.mfa.status");
export const identityMfaEnroll = (
  input: IdentityMfaEnrollInput,
  key: string,
  signal?: AbortSignal,
) => invoke<IdentityMfaEnrollOutput>("identity.mfa.enroll", input, key, signal);
export const identityMfaActivate = (
  input: IdentityMfaActivateInput,
  key: string,
  signal?: AbortSignal,
) =>
  invoke<IdentityMfaActivateOutput>(
    "identity.mfa.activate",
    input,
    key,
    signal,
  );

export type ActivityObservation = MemoryActivityCollectInput["events"][number];

export const collectActivity = (events: ActivityObservation[], key: string) =>
  invoke<MemoryActivityCollectOutput>(
    "memory.activity.collect",
    { events },
    key,
  );

export type MemoryPartition = MemoryItemCreateInput["partition"];
export type MemoryItem = MemoryItemGetOutput;
export type MemoryEvent = MemoryEventListOutput["events"][number];
export type MemorySource = MemoryItemSourcesOutput["sources"][number];

export const memoryCreate = (input: MemoryItemCreateInput, key?: string) =>
  invoke<MemoryItemCreateOutput>("memory.item.create", input, key);
export const memoryGet = (id: number) =>
  invoke<MemoryItemGetOutput>("memory.item.get", { id });
export const memoryList = (input: MemoryItemListInput = {}) =>
  invoke<MemoryItemListOutput>("memory.item.list", input);
export const memoryCorrect = (input: MemoryItemCorrectInput, key?: string) =>
  invoke<MemoryItemCorrectOutput>("memory.item.correct", input, key);
export const memoryArchive = (id: number, version: number, key?: string) =>
  invoke<MemoryItemArchiveOutput>("memory.item.archive", { id, version }, key);
export const memoryDelete = (id: number, version: number, key?: string) =>
  invoke<MemoryItemDeleteOutput>("memory.item.delete", { id, version }, key);
export const memorySources = (id: number) =>
  invoke<MemoryItemSourcesOutput>("memory.item.sources", { id });
export const memoryEvents = (input: MemoryEventListInput = {}) =>
  invoke<MemoryEventListOutput>("memory.event.list", input);
export const memoryIngestionFailures = (limit?: number) =>
  invoke<MemoryIngestionFailuresOutput>(
    "memory.ingestion.failures",
    limit === undefined ? {} : { limit },
  );
export const memoryReplay = (eventId: string, key?: string) =>
  invoke<MemoryIngestionReplayOutput>(
    "memory.ingestion.replay",
    { eventId },
    key,
  );
export const memoryReplayRange = (
  input: MemoryIngestionReplayRangeInput,
  key?: string,
) =>
  invoke<MemoryIngestionReplayRangeOutput>(
    "memory.ingestion.replay_range",
    input,
    key,
  );
export const projectMemoryCreate = (
  input: MemoryProjectCreateInput,
  key?: string,
) => invoke<MemoryProjectCreateOutput>("memory.project.create", input, key);
export const projectMemoryList = (input: MemoryProjectListInput) =>
  invoke<MemoryProjectListOutput>("memory.project.list", input);
export const projectMemoryEvents = (input: MemoryProjectEventsInput) =>
  invoke<MemoryProjectEventsOutput>("memory.project.events", input);
export const projectMemoryGet = (projectId: number, id: number) =>
  invoke<MemoryProjectGetOutput>("memory.project.get", { projectId, id });
export const projectMemorySources = (projectId: number, id: number) =>
  invoke<MemoryProjectSourcesOutput>("memory.project.sources", {
    projectId,
    id,
  });
export const projectMemoryCorrect = (
  input: MemoryProjectCorrectInput,
  key?: string,
) => invoke<MemoryProjectCorrectOutput>("memory.project.correct", input, key);
export const projectMemoryArchive = (
  projectId: number,
  id: number,
  version: number,
  key?: string,
) =>
  invoke<MemoryProjectArchiveOutput>(
    "memory.project.archive",
    { projectId, id, version },
    key,
  );
export const projectMemoryDelete = (
  projectId: number,
  id: number,
  version: number,
  key?: string,
) =>
  invoke<MemoryProjectDeleteOutput>(
    "memory.project.delete",
    { projectId, id, version },
    key,
  );
export const memoryPolicies = () =>
  invoke<MemoryPolicyListOutput>("memory.policy.list");
export const memoryPolicySet = (input: MemoryPolicySetInput, key?: string) =>
  invoke<MemoryPolicySetOutput>("memory.policy.set", input, key);
export const groupMemoryCreate = (
  input: MemoryGroupCreateInput,
  key?: string,
) => invoke<MemoryGroupCreateOutput>("memory.group.create", input, key);
export const groupMemoryList = (input: MemoryGroupListInput) =>
  invoke<MemoryGroupListOutput>("memory.group.list", input);
export const groupMemoryGet = (input: MemoryGroupGetInput) =>
  invoke<MemoryGroupGetOutput>("memory.group.get", input);
export const groupMemorySources = (input: MemoryGroupSourcesInput) =>
  invoke<MemoryGroupSourcesOutput>("memory.group.sources", input);
export const groupMemoryCorrect = (
  input: MemoryGroupCorrectInput,
  key?: string,
) => invoke<MemoryGroupCorrectOutput>("memory.group.correct", input, key);
export const groupMemoryArchive = (
  input: MemoryGroupArchiveInput,
  key?: string,
) => invoke<MemoryGroupArchiveOutput>("memory.group.archive", input, key);
export const groupMemoryDelete = (
  input: MemoryGroupDeleteInput,
  key?: string,
) => invoke<MemoryGroupDeleteOutput>("memory.group.delete", input, key);
export const entityMemoryCreate = (
  input: MemoryEntityCreateInput,
  key?: string,
) => invoke<MemoryEntityCreateOutput>("memory.entity.create", input, key);
export const entityMemoryList = (input: MemoryEntityListInput) =>
  invoke<MemoryEntityListOutput>("memory.entity.list", input);
export const entityMemoryGet = (input: MemoryEntityGetInput) =>
  invoke<MemoryEntityGetOutput>("memory.entity.get", input);
export const entityMemorySources = (input: MemoryEntitySourcesInput) =>
  invoke<MemoryEntitySourcesOutput>("memory.entity.sources", input);
export const entityMemoryCorrect = (
  input: MemoryEntityCorrectInput,
  key?: string,
) => invoke<MemoryEntityCorrectOutput>("memory.entity.correct", input, key);
export const entityMemoryArchive = (
  input: MemoryEntityArchiveInput,
  key?: string,
) => invoke<MemoryEntityArchiveOutput>("memory.entity.archive", input, key);
export const entityMemoryDelete = (
  input: MemoryEntityDeleteInput,
  key?: string,
) => invoke<MemoryEntityDeleteOutput>("memory.entity.delete", input, key);

function identifier(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return `cap-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

/**
 * invoke runs one capability and unwraps its business result. A denial or a
 * validation problem is thrown as ApiError so callers keep their existing error
 * handling; a business failure is never reported as success.
 */
async function invoke<T>(
  name: string,
  input: Record<string, unknown> = {},
  idempotencyKey?: string,
  signal?: AbortSignal,
): Promise<CapabilityResponse<T>> {
  const response = await capabilityInvoke(
    name,
    {
      input,
      idempotencyKey: idempotencyKey ?? identifier(),
    },
    { signal },
  );
  const result = response.data as CapabilityResult;
  if (result.status === "awaiting_approval") {
    throw new ApiError("approval_required", 409);
  }
  if (result.status !== "succeeded") {
    throw new ApiError(result.errorCode ?? "internal", 400);
  }
  return {
    status: 200,
    data: result.data as T,
    headers: response.headers ?? emptyHeaders,
  };
}

// ---------------------------------------------------------------- cloud drive

export type DriveSpace = {
  id: number;
  kind: "personal" | "shared";
  name: string;
  role: "admin" | "editor" | "viewer";
};
export type DriveFolder = { id: number; name: string; parentId: number };
export type DriveFile = {
  id: number;
  name: string;
  folderId: number;
  size: number;
  sha256: string;
  createdAt: string;
};
export type DriveMember = {
  id: number;
  targetType: "account" | "department";
  targetId: number;
  role: "admin" | "editor" | "viewer";
};
export type DriveTrashItem = {
  id: number;
  kind: "folder" | "file";
  folderId: number;
  fileId: number;
  name: string;
  parentId: number;
  deletedAt: string;
  expiresAt: string;
};
export type DriveUploadStatus = {
  id: number;
  spaceId: number;
  folderId: number;
  name: string;
  size: number;
  state: string;
  expiresAt: string;
  completedFileId: number;
  completedChunks: { index: number; sha256: string }[];
};
export type DriveFileChunk = {
  name: string;
  size: number;
  sha256: string;
  offset: number;
  content: string;
  eof: boolean;
};

export const drivePersonalEnsure = () =>
  invoke<{ id: number }>("drive.space.personal.ensure");
export const driveSpaces = () =>
  invoke<{ items: DriveSpace[] }>("drive.space.list");
export const driveSpaceUsage = (spaceId: number) =>
  invoke<{ usedBytes: number; pendingBytes: number; quotaBytes: number }>(
    "drive.space.usage",
    { spaceId },
  );
export const driveSpaceCreate = (name: string) =>
  invoke<{ id: number }>("drive.space.create", { name });
export const driveFolders = (spaceId: number, parentId: number) =>
  invoke<{ items: DriveFolder[] }>("drive.folder.list", { spaceId, parentId });
export const driveFolderPath = (spaceId: number, folderId: number) =>
  invoke<{ items: DriveFolder[] }>("drive.folder.path", { spaceId, folderId });
export const driveFolderCreate = (
  spaceId: number,
  parentId: number,
  name: string,
) => invoke<{ id: number }>("drive.folder.create", { spaceId, parentId, name });
export const driveFolderRename = (spaceId: number, id: number, name: string) =>
  invoke<{ id: number }>("drive.folder.rename", { spaceId, id, name });
export const driveFolderMove = (
  spaceId: number,
  id: number,
  parentId: number,
) => invoke<{ id: number }>("drive.folder.move", { spaceId, id, parentId });
export const driveFolderTrash = (spaceId: number, folderId: number) =>
  invoke<{ id: number }>("drive.folder.trash", { spaceId, folderId });
export const driveFiles = (spaceId: number, folderId: number) =>
  invoke<{ items: DriveFile[] }>("drive.file.list", { spaceId, folderId });
export const driveFileRename = (
  spaceId: number,
  fileId: number,
  name: string,
) => invoke<{ id: number }>("drive.file.rename", { spaceId, fileId, name });
export const driveFileMove = (
  spaceId: number,
  fileId: number,
  folderId: number,
) => invoke<{ id: number }>("drive.file.move", { spaceId, fileId, folderId });
export const driveFileTrash = (spaceId: number, fileId: number) =>
  invoke<{ id: number }>("drive.file.trash", { spaceId, fileId });
export const driveMembers = (spaceId: number) =>
  invoke<{ items: DriveMember[] }>("drive.member.list", { spaceId });
export const driveMemberAdd = (
  spaceId: number,
  targetType: DriveMember["targetType"],
  targetId: number,
  role: DriveMember["role"],
) =>
  invoke<{ id: number }>("drive.member.add", {
    spaceId,
    targetType,
    targetId,
    role,
  });
export const driveMemberChange = (
  spaceId: number,
  memberId: number,
  role: DriveMember["role"],
) => invoke<{ id: number }>("drive.member.change", { spaceId, memberId, role });
export const driveMemberRemove = (spaceId: number, memberId: number) =>
  invoke<{ id: number }>("drive.member.remove", { spaceId, memberId });
export const driveTrash = (spaceId: number) =>
  invoke<{ items: DriveTrashItem[] }>("drive.trash.list", { spaceId });
export const driveFolderRestore = (
  spaceId: number,
  batchId: number,
  parentId: number,
  name: string,
) =>
  invoke<{ id: number }>("drive.trash.restore", {
    spaceId,
    batchId,
    parentId,
    name,
  });
export const driveFileRestore = (
  spaceId: number,
  batchId: number,
  folderId: number,
  name: string,
) =>
  invoke<{ id: number }>("drive.trash.file.restore", {
    spaceId,
    batchId,
    folderId,
    name,
  });
export const driveTrashPurge = (spaceId: number, batchId: number) =>
  invoke<{ id: number }>("drive.trash.purge.request", { spaceId, batchId });
export const driveUploadStart = (
  spaceId: number,
  folderId: number,
  name: string,
  size: number,
  key: string,
) =>
  invoke<{ id: number; chunkSize: number }>(
    "drive.upload.start",
    { spaceId, folderId, name, size },
    key,
  );
export const driveUploadStatus = (id: number) =>
  invoke<DriveUploadStatus>("drive.upload.status", { id });
export const driveUploadChunk = (
  spaceId: number,
  uploadId: number,
  index: number,
  sha256: string,
  content: string,
  key: string,
) =>
  invoke<{ index: number }>(
    "drive.upload.chunk",
    { spaceId, uploadId, index, sha256, content },
    key,
  );
export const driveUploadComplete = (
  spaceId: number,
  uploadId: number,
  key: string,
) =>
  invoke<{ id: number }>("drive.upload.complete", { spaceId, uploadId }, key);
export const driveUploadCancel = (spaceId: number, uploadId: number) =>
  invoke<{ id: number }>("drive.upload.cancel", { spaceId, uploadId });
export const driveFileChunk = (
  spaceId: number,
  fileId: number,
  offset: number,
) =>
  invoke<DriveFileChunk>("drive.file.chunk.read", { spaceId, fileId, offset });

function body(value: unknown): Record<string, unknown> {
  return (value ?? {}) as Record<string, unknown>;
}

// ---------------------------------------------------------------- settings

type Target = { targetKind?: string; targetId?: number };

export const settingsWorkspace = (params?: Target) =>
  invoke<SettingsWorkspace>("settings.workspace.read", { ...params });

export const settingsEffective = () =>
  invoke<EffectiveConfiguration>("settings.effective.read");

export const settingsPreferences = (value: unknown) =>
  invoke<EffectiveConfiguration>("settings.preferences.update", body(value));

export const settingsDraft = (value: unknown) =>
  invoke<ChangeRecord>("settings.draft.create", body(value));

export const settingsPublish = (id: number, value: unknown) =>
  invoke<ChangeRecord>("settings.draft.publish", { id, ...body(value) });

export const settingsRollback = (id: number, value: unknown) =>
  invoke<ChangeRecord>("settings.history.rollback", { id, ...body(value) });

export const settingsSchedules = () =>
  invoke<SettingSchedule[]>("settings.schedule.list");

export const settingsSchedule = (id: number, value: unknown) =>
  invoke<SettingSchedule>("settings.schedule.create", { id, ...body(value) });

export const settingsCancelSchedule = (id: number) =>
  invoke<{ id: number }>("settings.schedule.cancel", { id });

export const settingsExport = (params?: Target) =>
  invoke<ChangeEntry[]>("settings.export.read", { ...params });

export const settingsLogo = () => invoke<FileContent>("settings.logo.read");

export const settingsReauthenticate = (value: unknown) =>
  invoke<ReauthProof>("settings.password.verify", body(value));

// ------------------------------------------------------------ dictionaries

type DictionaryQuery = Target;

export const dictionariesGet = (id: number, params?: DictionaryQuery) =>
  invoke<DictionaryDetail>("dictionary.detail.read", { id, ...params });

export const dictionariesOptions = (
  code: string,
  params?: { language?: string; ids?: number[]; search?: string },
) => invoke<DictionaryOption[]>("dictionary.options.read", { code, ...params });

export const dictionariesDraft = (value: unknown) =>
  invoke<ChangeRecord>("dictionary.draft.create", body(value));

export const dictionariesPublish = (id: number, value: unknown) =>
  invoke<ChangeRecord>("dictionary.draft.publish", { id, ...body(value) });

export const dictionariesRollback = (id: number, value: unknown) =>
  invoke<ChangeRecord>("dictionary.history.rollback", { id, ...body(value) });

export const dictionariesRemove = (id: number) =>
  invoke<{ id: number }>("dictionary.delete", { id });

export const dictionariesOverride = (id: number, value: unknown) =>
  invoke<ChangeRecord>("dictionary.override.save", { id, ...body(value) });

export const dictionariesImport = (id: number, value: unknown) =>
  invoke<ImportPreview>("dictionary.import.preview", { id, ...body(value) });

export const dictionariesImportDraft = (id: number, value: unknown) =>
  invoke<ChangeRecord>("dictionary.import.draft", { id, ...body(value) });

export const dictionariesExport = (id: number, params?: { format?: string }) =>
  invoke<{ filename: string; content: string }>("dictionary.export.read", {
    id,
    ...params,
  });

// ------------------------------------------------------------------- files

export const filesList = () => invoke<ManagedFile[]>("file.list.read");

export const filesDrivers = () => invoke<StorageDriver[]>("file.drivers.read");

export const filesUpload = (value: unknown) =>
  invoke<ManagedFile>("file.upload", body(value));

export const filesContent = (id: number) =>
  invoke<FileContent>("file.content.read", { id });

export const filesRemove = (id: number) =>
  invoke<{ id: number }>("file.delete", { id });

// ----------------------------------------------------------- notifications

export const notificationsAudience = () =>
  invoke<NotificationAudience[]>("notification.audience.read");

export const notificationsPreview = (value: unknown) =>
  invoke<{ recipientCount: number }>("notification.preview.read", body(value));

export const notificationsSend = (value: unknown) =>
  invoke<SentNotification>("notification.send", body(value));

export const notificationsSent = () =>
  invoke<SentNotification[]>("notification.sent.read");

export const notificationsAttachment = (id: number, fileId: number) =>
  invoke<FileContent>("notification.attachment.read", { id, fileId });

export const notificationsMessage = (id: number) =>
  invoke<InboxMessage>("notification.message.read", { id });

export const notificationsInbox = () =>
  invoke<InboxMessage[]>("notification.inbox.read");

export const notificationsRead = (id: number) =>
  invoke<{ id: number }>("notification.inbox.mark_read", { id });

export const notificationsDeliveries = () =>
  invoke<DeliveryRecord[]>("notification.delivery.read");

export const notificationsRetry = (id: number) =>
  invoke<{ id: number }>("notification.delivery.retry", { id });

export const notificationsTest = (value: unknown) =>
  invoke<{ status: string }>("notification.smtp.test", body(value));

// --------------------------------------------------------------------- mcp

export const mcpManagementCreate = (value: unknown, signal?: AbortSignal) =>
  invoke<MCPTokenCreated>("mcp.token.create", body(value), undefined, signal);

export const mcpManagementRevoke = (id: number, value: unknown) =>
  invoke<{ id: number }>("mcp.token.revoke", { id, ...body(value) });

export const mcpManagementRotate = (id: number, value: unknown) =>
  invoke<MCPTokenCreated>("mcp.token.rotate", { id, ...body(value) });

export const mcpManagementAdminRevoke = (id: number, value: unknown) =>
  invoke<{ id: number }>("mcp.token.admin_revoke", { id, ...body(value) });

export const mcpManagementCapabilities = () =>
  invoke<{ tools: string[] }>("mcp.capability.list", {});

// ---------------------------------------------------------------- identity

export const identityCreateAccount = (value: unknown) =>
  invoke<Record<string, unknown>>("identity.account.create", body(value));

export const identityUpdateAccount = (id: number, value: unknown) =>
  invoke<Record<string, unknown>>("identity.account.update", {
    id,
    ...body(value),
  });

export const identityDeleteAccount = (id: number) =>
  invoke<{ id: number }>("identity.account.delete", { id });

export const identityUpdateOrganization = (id: number, value: unknown) =>
  invoke<Record<string, unknown>>("identity.organization.update", {
    id,
    ...body(value),
  });

export const identityCreateDepartment = (value: unknown) =>
  invoke<Record<string, unknown>>("identity.department.create", body(value));

export const identityUpdateDepartment = (id: number, value: unknown) =>
  invoke<Record<string, unknown>>("identity.department.update", {
    id,
    ...body(value),
  });

export const identityDeleteDepartment = (id: number) =>
  invoke<{ id: number }>("identity.department.delete", { id });

export const identityCreateRole = (value: unknown) =>
  invoke<Record<string, unknown>>("identity.role.create", body(value));

export const identityUpdateRole = (id: number, value: unknown) =>
  invoke<Record<string, unknown>>("identity.role.update", {
    id,
    ...body(value),
  });

export const identityDeleteRole = (id: number) =>
  invoke<{ id: number }>("identity.role.delete", { id });

export const capabilityKey = (name: string) => ["capability", name] as const;

// Knowledge base management. Retrieval stays a separate capability so the agent
// can search without being able to index or delete.
export const knowledgeList = () =>
  invoke<KnowledgeDocument[]>("knowledge.list");

export const knowledgeQuery = (
  input: KnowledgeQueryInput,
  signal?: AbortSignal,
) => invoke<KnowledgeQueryOutput>("knowledge.query", input, undefined, signal);

export const knowledgeRead = (
  input: KnowledgeReadInput,
  signal?: AbortSignal,
) => invoke<KnowledgeReadOutput>("knowledge.read", input, undefined, signal);

export const knowledgeUpdate = (input: KnowledgeUpdateInput) =>
  invoke<KnowledgeUpdateOutput>("knowledge.update", input);

export const knowledgeCreate = (input: { title: string; content: string }) =>
  invoke<KnowledgeDocument>("knowledge.create", input);

export const knowledgeDelete = (id: number) =>
  invoke<{ id: number }>("knowledge.delete", { id });

export const knowledgeReindex = () =>
  invoke<{ documents: number }>("knowledge.reindex");

// Keep the Gateway envelope for flows that can legitimately await approval.
export async function invokeCapability(
  name: string,
  input: Record<string, unknown>,
  idempotencyKey?: string,
  version?: string,
): Promise<CapabilityResult> {
  const response = await capabilityInvoke(name, {
    input,
    idempotencyKey,
    version,
  });
  if (response.status !== 200)
    throw new ApiError("unavailable", response.status);
  const result = response.data;
  if (result.status !== "succeeded" && result.status !== "awaiting_approval")
    throw new ApiError(result.errorCode || "internal", 400);
  return result;
}

// ---------------------------------------------------------------- projects
// Every call below is a capability: the page never composes SQL or bypasses the
// gateway, and membership is decided by the server, not by which buttons render.

export type Project = {
  id: number;
  code: string;
  name: string;
  status: string;
  priority: string;
  dueDate: string;
  ownerId: number;
  ownerName: string;
  planCount: number;
  taskCount: number;
  taskDoneCount: number;
  taskOverdueCount: number;
  createdAt: string;
  updatedAt: string;
};

export type ProjectMember = {
  accountId: number;
  name: string;
  email: string;
  isOwner: boolean;
};

export type ProjectAccount = {
  accountId: number;
  name: string;
  email: string;
};

export type ProjectPlan = {
  id: number;
  projectId: number;
  planType: string;
  title: string;
  content?: string;
  status: string;
  priority: string;
  dueDate: string;
  ownerId: number;
  ownerName: string;
};

export type ProjectTask = {
  id: number;
  projectId: number;
  planId: number;
  planTitle: string;
  title: string;
  description?: string;
  status: string;
  priority: string;
  assigneeId: number;
  assigneeName: string;
  dueDate: string;
  completedAt: string;
  brief?: string;
  briefModel?: string;
};

export type ProjectPlanInput = {
  planType: string;
  title: string;
  content: string;
  priority: string;
  dueDate: string;
  ownerId: number;
};

export type ProjectTaskInput = {
  planId: number;
  title: string;
  description: string;
  priority: string;
  assigneeId: number;
  dueDate: string;
};

export const projectsList = (params?: {
  status?: string;
  priority?: string;
  keyword?: string;
  limit?: number;
}) => invoke<{ items: Project[] }>("project.list", { ...params });

export const projectGet = (id: number) =>
  invoke<Project>("project.get", { id });

export const projectCreate = (value: {
  code: string;
  name: string;
  priority: string;
  dueDate: string;
}) => invoke<{ id: number }>("project.create", body(value));

export const projectUpdate = (value: {
  id: number;
  name: string;
  priority: string;
  dueDate: string;
}) => invoke<{ id: number }>("project.update", body(value));

export const projectStatusChange = (id: number, from: string, status: string) =>
  invoke<{ id: number; status: string }>("project.status.change", {
    id,
    from,
    status,
  });

export const projectMembers = (projectId: number) =>
  invoke<{ items: ProjectMember[] }>("project.member.list", { projectId });

// The directory search exists because a plain member has no accounts:read.
export const projectMemberSearch = (projectId: number, keyword: string) =>
  invoke<{ items: ProjectAccount[] }>("project.member.search", {
    projectId,
    keyword,
  });

export const projectMemberAdd = (projectId: number, accountId: number) =>
  invoke<{ projectId: number; accountId: number; added: boolean }>(
    "project.member.add",
    { projectId, accountId },
  );

export const projectMemberRemove = (projectId: number, accountId: number) =>
  invoke<{ projectId: number; accountId: number }>("project.member.remove", {
    projectId,
    accountId,
  });

export const projectOwnerTransfer = (projectId: number, accountId: number) =>
  invoke<{ projectId: number; ownerId: number }>("project.owner.transfer", {
    projectId,
    accountId,
  });

export const projectPlans = (projectId: number) =>
  invoke<{ items: ProjectPlan[] }>("project.plan.list", { projectId });

export const projectPlanGet = (id: number) =>
  invoke<ProjectPlan>("project.plan.get", { id });

export const projectPlanCreate = (
  value: ProjectPlanInput & { projectId: number },
) => invoke<{ id: number }>("project.plan.create", body(value));

export const projectPlanUpdate = (value: ProjectPlanInput & { id: number }) =>
  invoke<{ id: number }>("project.plan.update", body(value));

export const projectPlanStatusChange = (
  id: number,
  from: string,
  status: string,
) =>
  invoke<{ id: number; status: string }>("project.plan.status.change", {
    id,
    from,
    status,
  });

export const projectTasks = (
  projectId: number,
  params?: {
    planId?: number;
    assigneeId?: number;
    status?: string;
    limit?: number;
  },
) =>
  invoke<{ items: ProjectTask[] }>("project.task.list", {
    projectId,
    ...params,
  });

export const projectTaskGet = (id: number) =>
  invoke<ProjectTask>("project.task.get", { id });

export const projectTaskCreate = (
  value: ProjectTaskInput & { projectId: number },
) => invoke<{ id: number }>("project.task.create", body(value));

export const projectTaskUpdate = (value: ProjectTaskInput & { id: number }) =>
  invoke<{ id: number }>("project.task.update", body(value));

export const projectTaskStatusChange = (
  id: number,
  from: string,
  status: string,
) =>
  invoke<{ id: number; status: string; completedAt: string }>(
    "project.task.status.change",
    { id, from, status },
  );

export const projectTaskAssign = (id: number, assigneeId: number) =>
  invoke<{ id: number; assigneeId: number }>("project.task.assign", {
    id,
    assigneeId,
  });

// Runtime reads remain capabilities; metadata never supplies an arbitrary URL.
export const runtimeApplication = (
  applicationId: string,
  signal?: AbortSignal,
) => runtimeRead("application.runtime.resolve", { applicationId }, signal);
export const runtimeRead = (
  name: string,
  input: Record<string, unknown>,
  signal?: AbortSignal,
  version = "1.0.0",
) =>
  capabilityInvoke(name, { input, version }, { signal }).then((response) => {
    if (response.status !== 200)
      throw new ApiError("unavailable", response.status);
    if (response.data.status !== "succeeded")
      throw new ApiError(response.data.errorCode || "internal", 400);
    return response.data.data as unknown;
  });
export const runtimeSave = (
  name: string,
  input: Record<string, unknown>,
  key: string,
) => invokeCapability(name, input, key, "1.0.0");

// ---------------------------------------------------------------- mail
export type MailChannel = {
  direction: "send" | "receive";
  protocol: "smtp" | "pop3" | "imap";
  host: string;
  port: number;
  tlsMode: "starttls" | "tls";
  username: string;
  verificationStatus: "unknown" | "ok" | "failed";
  lastErrorCode?: string;
  lastVerifiedAt?: string;
  credential: { version: number; fingerprint: string };
};

export type Mailbox = {
  id: number;
  name: string;
  emailAddress: string;
  defaultSend: boolean;
  status: "unverified" | "active" | "disabled";
  revision: number;
  createdAt: string;
  updatedAt: string;
  channels: MailChannel[];
};

export type MailAddress = { address: string; displayName?: string };
export type MailAttachment = {
  id: number;
  fileId: number;
  filename: string;
  mime: string;
  size: number;
  disposition: "attachment" | "inline";
};
export type MailMessageListItem = {
  id: number;
  mailboxId: number;
  direction: "inbound" | "outbound";
  folder: "inbox" | "sent";
  subject: string;
  from: string;
  snippet: string;
  occurredAt: string;
  read: boolean;
};
export type MailMessage = MailMessageListItem & {
  rfcMessageId: string;
  replyTo: string;
  bodyText: string;
  bodyHtml: string;
  createdAt: string;
  recipients: {
    kind: "to" | "cc" | "bcc";
    address: string;
    displayName: string;
  }[];
  attachments: MailAttachment[];
};
export type MailDelivery = {
  id: number;
  mailboxId: number;
  messageId: number;
  status: "queued" | "sending" | "sent" | "failed" | "ambiguous" | "cancelled";
  attempts: number;
  nextAttemptAt: string;
  errorCode: string;
  sentAt: string;
  createdAt: string;
  subject: string;
  from: string;
};

export const mailMailboxList = (signal?: AbortSignal) =>
  invoke<{ items: Mailbox[] }>("mail.mailbox.list", {}, undefined, signal);
type MailReceiveInput =
  | { pop3Host: string; pop3Port: number; pop3Username?: string }
  | {
      receiveProtocol: "pop3" | "imap";
      receiveHost: string;
      receivePort: number;
      receiveTls: "starttls" | "tls";
      receiveUsername?: string;
    };
export const mailMailboxOnboard = (
  input: MailReceiveInput & {
    name: string;
    emailAddress: string;
    password: string;
    smtpHost: string;
    smtpPort: number;
    smtpTls: "starttls" | "tls";
    smtpUsername?: string;
  },
) => invoke<{ id: number; status: string }>("mail.mailbox.onboard", input);
export const mailMailboxUpdate = (
  input: MailReceiveInput & {
    id: number;
    revision: number;
    name: string;
    emailAddress: string;
    defaultSend: boolean;
    smtpHost: string;
    smtpPort: number;
    smtpTls: "starttls" | "tls";
    smtpUsername?: string;
  },
) => invoke<{ id: number }>("mail.mailbox.update", input);
export const mailCredentialReplace = (
  mailboxId: number,
  purpose: MailChannel["protocol"],
  password: string,
) =>
  invoke<{ mailboxId: number; purpose: string; version: number }>(
    "mail.mailbox.credential.replace",
    { mailboxId, purpose, password },
  );
export const mailMailboxEnable = (id: number) =>
  invoke<{ id: number; status: string }>("mail.mailbox.enable", { id });
export const mailMailboxDisable = (id: number) =>
  invoke<{ id: number; status: string }>("mail.mailbox.disable", { id });
export const mailConnectionTest = (
  mailboxId: number,
  direction: "send" | "receive",
) =>
  invoke<{ mailboxId: number; direction: string; status: string }>(
    "mail.mailbox.connection.test",
    { mailboxId, direction },
  );
export const mailMessageList = (
  input: {
    folder: "inbox" | "sent";
    mailboxId?: number;
    page?: number;
    pageSize?: number;
  },
  signal?: AbortSignal,
) =>
  invoke<{ items: MailMessageListItem[]; total: number }>(
    "mail.message.list",
    input,
    undefined,
    signal,
  );
export const mailMessageGet = (id: number, signal?: AbortSignal) =>
  invoke<MailMessage>("mail.message.get", { id }, undefined, signal);
export const mailMessageMarkRead = (id: number) =>
  invoke<{ id: number; read: boolean }>("mail.message.flag.update", {
    id,
    read: true,
  });
export const mailMessageSend = (
  input: (
    { markdown: string; html?: never } | { html: string; markdown?: never }
  ) & {
    clientKey: string;
    mailboxId: number;
    to: MailAddress[];
    cc?: MailAddress[];
    bcc?: MailAddress[];
    subject: string;
    replyTo?: string;
    fileIds?: number[];
  },
) =>
  invoke<{ messageId: number; deliveryId: number; status: string }>(
    "mail.message.send",
    input,
  );
export const mailDeliveryList = (signal?: AbortSignal) =>
  invoke<{ items: MailDelivery[] }>(
    "mail.delivery.list",
    {},
    undefined,
    signal,
  );
export const mailDeliveryRetry = (id: number) =>
  invoke<{ id: number; status: string }>("mail.delivery.retry", { id });
export const mailDeliveryCancel = (id: number) =>
  invoke<{ id: number; status: string }>("mail.delivery.cancel", { id });
export const mailAttachmentRead = (messageId: number, attachmentId: number) =>
  invoke<{ name: string; mime: string; content: string }>(
    "mail.attachment.read",
    { messageId, attachmentId },
  );

export const runtimeExecute = (
  name: string,
  version: string,
  input: Record<string, unknown>,
  idempotencyKey: string,
  correlationId: string,
  runtime?: import("@/api/generated/models").RuntimeInvocationReference,
) =>
  capabilityInvoke(name, {
    version,
    input,
    idempotencyKey,
    correlationId,
    runtime,
  }).then((response) => {
    if (response.status !== 200)
      throw new ApiError("unavailable", response.status);
    return response.data;
  });

// Execution-plan DTOs are generated from module Definition schemas.
export const executionPlans = {
  list: (input: EP.ExecutionPlanListInput, signal?: AbortSignal) =>
    invoke<EP.ExecutionPlanListOutput>(
      "execution.plan.list",
      input,
      undefined,
      signal,
    ),
  binding: (input: EP.ExecutionPlanBindingInput, signal?: AbortSignal) =>
    invoke<EP.ExecutionPlanBindingOutput>(
      "execution.plan.binding",
      input,
      undefined,
      signal,
    ),
  history: (input: EP.ExecutionPlanHistoryInput, signal?: AbortSignal) =>
    invoke<EP.ExecutionPlanHistoryOutput>(
      "execution.plan.history",
      input,
      undefined,
      signal,
    ),
  records: (input: EP.ExecutionPlanRecordsInput, signal?: AbortSignal) =>
    invoke<EP.ExecutionPlanRecordsOutput>(
      "execution.plan.records",
      input,
      undefined,
      signal,
    ),
  references: (input: EP.ExecutionPlanReferencesInput, signal?: AbortSignal) =>
    invoke<EP.ExecutionPlanReferencesOutput>(
      "execution.plan.references",
      input,
      undefined,
      signal,
    ),
  inspect: (input: EP.ExecutionPlanInspectInput, signal?: AbortSignal) =>
    invoke<EP.ExecutionPlanInspectOutput>(
      "execution.plan.inspect",
      input,
      undefined,
      signal,
    ),
  trace: (input: EP.ExecutionPlanTraceInput, signal?: AbortSignal) =>
    invoke<EP.ExecutionPlanTraceOutput>(
      "execution.plan.trace",
      input,
      undefined,
      signal,
    ),
  create: (input: EP.ExecutionPlanCreateInput, key: string) =>
    invoke<EP.ExecutionPlanCreateOutput>("execution.plan.create", input, key),
  validate: (input: EP.ExecutionPlanValidateInput, key: string) =>
    invoke<EP.ExecutionPlanValidateOutput>(
      "execution.plan.validate",
      input,
      key,
    ),
  publish: (input: EP.ExecutionPlanPublishInput, key: string) =>
    invoke<EP.ExecutionPlanPublishOutput>("execution.plan.publish", input, key),
  bind: (input: EP.ExecutionPlanBindInput, key: string) =>
    invoke<EP.ExecutionPlanBindOutput>("execution.plan.bind", input, key),
  rollback: (input: EP.ExecutionPlanRollbackInput, key: string) =>
    invoke<EP.ExecutionPlanRollbackOutput>(
      "execution.plan.rollback",
      input,
      key,
    ),
  preflight: (input: EP.ExecutionPlanPreflightInput) =>
    invoke<EP.ExecutionPlanPreflightOutput>("execution.plan.preflight", input),
};

export const previewIdentityRole = (
  input: Record<string, unknown>,
  signal?: AbortSignal,
) =>
  invoke<IAM.IdentityRolePreviewOutput>(
    "identity.role.preview",
    input,
    undefined,
    signal,
  );

export const listIAMResources = (
  input: IAM.IdentityResourceListInput,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityResourceListOutput>(
    "identity.resource.list",
    input,
    undefined,
    signal,
  );

export const setIAMResourceStatus = (
  input: IAM.IdentityResourceSetStatusInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityResourceSetStatusOutput>(
    "identity.resource.setStatus",
    input,
    key,
    signal,
  );

export const readAuditExportPolicy = (signal: AbortSignal) =>
  invoke<IAM.IdentityAuditExportPolicyOutput>(
    "identity.audit.exportPolicy",
    {},
    undefined,
    signal,
  );

export const listAuditExportPermits = (signal: AbortSignal) =>
  invoke<IAM.IdentityAuditExportPermitsOutput>(
    "identity.audit.exportPermits",
    {},
    undefined,
    signal,
  );

export const configureAuditExport = (
  input: IAM.IdentityAuditConfigureExportInput,
  key: string,
  signal: AbortSignal,
) =>
  invoke<IAM.IdentityAuditConfigureExportOutput>(
    "identity.audit.configureExport",
    input,
    key,
    signal,
  );

export async function requestAuditExport(
  input: IAM.IdentityAuditRequestExportInput,
  key: string,
  signal: AbortSignal,
) {
  const response = await capabilityInvoke(
    "identity.audit.requestExport",
    { input, idempotencyKey: key },
    { signal },
  );
  if (response.status !== 200)
    throw new ApiError("unavailable", response.status);
  if (response.data.status !== "awaiting_approval")
    throw new ApiError(response.data.errorCode || "internal", 400);
  return response.data;
}

export const previewContextRoleRevocation = (id: string, signal: AbortSignal) =>
  invoke<IAM.IdentityRoleGrantPreviewRevokeOutput>(
    "identity.roleGrant.previewRevoke",
    { id },
    undefined,
    signal,
  );
