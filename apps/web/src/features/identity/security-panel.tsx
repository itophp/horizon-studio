import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import {
  History,
  KeyRound,
  LogOut,
  Mail,
  Unlink,
  Users,
  Bot,
  RefreshCw,
} from "lucide-react";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import {
  identitySessions,
  identitySessionReauthenticate,
  identitySessionRevoke,
  externalIdentityList,
  externalIdentityUnlink,
} from "@/lib/capabilities";
import { TablePanelComponent } from "@/features/application-runtime/protocol/admin-workbench";
import type { ComponentRendererProps } from "@/features/application-runtime/protocol/component-registry";
import { useAuth, useSession } from "./auth";
import { ErrorBanner, Loading, RowActions } from "./shared";
import { SecurityConfirm } from "./security-confirm";
import { SecurityContacts } from "./security-contacts";
import { SecurityMFA } from "./security-mfa";
import { FederationProviders } from "./federation-providers";
import { SecurityAudit } from "./security-audit";
import { SecurityMemberships } from "./security-memberships";
import { SecurityWorkspaceMembers } from "./security-workspace-members";
import { ExternalIdentityReviews } from "./external-identity-reviews";
import { AgentDelegations } from "./agent-delegations";
import { LoginAssociations } from "./login-associations";

export function SecurityPanel(renderer: ComponentRendererProps) {
  const { t } = useTranslation();
  // Preview must neither query personal data nor expose credentials/actions.
  return renderer.readOnly ? (
    <p>{t("runtime:design.previewHint")}</p>
  ) : (
    <PersonalSecurity {...renderer} />
  );
}

function PersonalSecurity(renderer: ComponentRendererProps) {
  const { t } = useTranslation();
  const session = useSession();
  const [params, setParams] = useSearchParams();
  const manageMembers =
    session.permissions.includes("access:manage") && !session.identityBindingId;
  const tabs = [
    "devices",
    "mfa",
    "contacts",
    "external",
    ...(!session.identityBindingId ? ["associated-logins"] : []),
    "audit",
    "delegations",
    "memberships",
    ...(manageMembers ? ["workspace-members"] : []),
  ];
  const tab = tabs.includes(params.get("tab") ?? "")
    ? params.get("tab")!
    : "devices";
  const key = [
    "iam.security",
    session.tenantId,
    session.account.id,
    session.identityBindingId,
    session.identityBindingId,
    session.authorizationVersion,
  ];
  return (
    <Tabs
      value={tab}
      onValueChange={(next) =>
        setParams((previous) => {
          const updated = new URLSearchParams(previous);
          updated.set("tab", String(next));
          updated.delete("q");
          updated.delete("page");
          updated.delete("before");
          updated.delete("auditScope");
          updated.delete("memberAfter");
          return updated;
        })
      }
    >
      <TabsList className="max-w-full flex-wrap h-auto">
        <TabsTrigger value="devices">
          <LogOut />
          {t("admin:security.devices")}
        </TabsTrigger>
        <TabsTrigger value="mfa">
          <KeyRound />
          {t("admin:security.authenticator")}
        </TabsTrigger>
        <TabsTrigger value="contacts">
          <Mail />
          {t("admin:recovery.contacts")}
        </TabsTrigger>
        <TabsTrigger value="external">
          <Unlink />
          {t("admin:federation.title")}
        </TabsTrigger>
        {!session.identityBindingId && (
          <TabsTrigger value="associated-logins">
            <Users />
            {t("admin:loginAssociation.title")}
          </TabsTrigger>
        )}
        <TabsTrigger value="audit">
          <History />
          {t("admin:security.audit.title")}
        </TabsTrigger>
        <TabsTrigger value="delegations">
          <Bot />
          {t("admin:delegations.title")}
        </TabsTrigger>
        <TabsTrigger value="memberships">
          <Users />
          {t("admin:security.memberships.title")}
        </TabsTrigger>
        {manageMembers && (
          <TabsTrigger value="workspace-members">
            <Users />
            {t("admin:security.workspaceMembers.title")}
          </TabsTrigger>
        )}
      </TabsList>
      <TabsContent value="devices">
        {tab === "devices" && (
          <DeviceSecurity renderer={renderer} queryKey={key} />
        )}
      </TabsContent>
      <TabsContent value="mfa">
        {tab === "mfa" && <SecurityMFA queryKey={key} />}
      </TabsContent>
      <TabsContent value="contacts">
        {tab === "contacts" && <SecurityContacts queryKey={key} />}
      </TabsContent>
      <TabsContent value="external">
        {tab === "external" && (
          <ExternalSecurity renderer={renderer} queryKey={key} />
        )}
      </TabsContent>
      {!session.identityBindingId && (
        <TabsContent value="associated-logins">
          {tab === "associated-logins" && (
            <LoginAssociations key={JSON.stringify(key)} queryKey={key} />
          )}
        </TabsContent>
      )}
      <TabsContent value="audit">
        {tab === "audit" && (
          <SecurityAudit key={JSON.stringify(key)} queryKey={key} />
        )}
      </TabsContent>
      <TabsContent value="delegations">
        {tab === "delegations" && (
          <AgentDelegations key={JSON.stringify(key)} queryKey={key} />
        )}
      </TabsContent>
      <TabsContent value="memberships">
        {tab === "memberships" && <SecurityMemberships queryKey={key} />}
      </TabsContent>
      {manageMembers && (
        <TabsContent value="workspace-members">
          {tab === "workspace-members" && (
            <SecurityWorkspaceMembers queryKey={key} />
          )}
        </TabsContent>
      )}
    </Tabs>
  );
}

function DeviceSecurity({
  renderer,
  queryKey,
}: {
  renderer: ComponentRendererProps;
  queryKey: readonly unknown[];
}) {
  const { t } = useTranslation();
  const { renewSession } = useAuth();
  const [renewing, setRenewing] = useState(false);
  const sessions = useQuery({
    queryKey: [...queryKey, "devices"],
    queryFn: identitySessions,
    retry: false,
  });
  const [selected, setSelected] = useState<{ id: string; device: string }>();
  if (sessions.isPending) return <Loading />;
  if (sessions.error)
    return (
      <ErrorBanner
        error={sessions.error}
        onRetry={() => void sessions.refetch()}
      />
    );
  return (
    <>
      <p className="muted-text">{t("admin:security.devicesHelp")}</p>
      <TablePanelComponent
        {...renderer}
        data={sessions.data?.data.items.map((item) => ({
          ...item,
          publicId: item.id,
        }))}
        props={{
          keyField: "publicId",
          searchFields: ["device", "tenantId"],
          columns: [
            { name: "device", labelKey: "admin:security.device" },
            { name: "tenantId", labelKey: "admin:security.tenant" },
            {
              name: "current",
              labelKey: "admin:security.current",
              display: "yesno",
            },
            {
              name: "lastSeenAt",
              labelKey: "admin:security.lastSeen",
              display: "date",
            },
            {
              name: "expiresAt",
              labelKey: "admin:security.expires",
              display: "date",
            },
          ],
        }}
        renderRowActions={(row) =>
          row.current === true ? (
            renewSession && (
              <RowActions
                actions={[
                  {
                    label: t("admin:security.renewSession"),
                    icon: <RefreshCw />,
                    onClick: () => setRenewing(true),
                  },
                ]}
              />
            )
          ) : (
            <RowActions
              actions={[
                {
                  label: t("admin:security.revokeDevice"),
                  icon: <LogOut />,
                  danger: true,
                  onClick: () =>
                    setSelected({
                      id: String(row.publicId),
                      device: String(row.device),
                    }),
                },
              ]}
            />
          )
        }
      />
      {renewing && renewSession && (
        <SecurityConfirm
          title={t("admin:security.renewSession")}
          description={t("admin:security.renewSessionHelp")}
          showFactor
          onClose={() => setRenewing(false)}
          onConfirm={async (password, _key, signal, factor) => {
            await renewSession(
              { password, factor: factor || undefined },
              signal,
            );
            if (!signal.aborted) await sessions.refetch();
          }}
        />
      )}
      {selected && (
        <SecurityConfirm
          title={t("admin:security.revokeDevice")}
          description={t("admin:security.revokeDeviceHelp", {
            device: selected.device,
          })}
          onClose={() => setSelected(undefined)}
          onConfirm={async (password, key, signal) => {
            const proof = await identitySessionReauthenticate(
              { id: selected.id, password },
              key + ":reauth",
              signal,
            );
            if (signal.aborted) return;
            await identitySessionRevoke(
              { id: selected.id, proof: proof.data.proof },
              key + ":revoke",
              signal,
            );
            if (!signal.aborted) await sessions.refetch();
          }}
        />
      )}
    </>
  );
}

function ExternalSecurity({
  renderer,
  queryKey,
}: {
  renderer: ComponentRendererProps;
  queryKey: readonly unknown[];
}) {
  const { t } = useTranslation();
  const linked = useQuery({
    queryKey: [...queryKey, "external"],
    queryFn: externalIdentityList,
    retry: false,
  });
  const [selected, setSelected] = useState<{ id: string; provider: string }>();
  if (linked.isPending) return <Loading />;
  if (linked.error)
    return (
      <ErrorBanner error={linked.error} onRetry={() => void linked.refetch()} />
    );
  return (
    <>
      <p className="muted-text">{t("admin:security.externalHelp")}</p>
      <FederationProviders purpose="link" />
      <ExternalIdentityReviews
        key={JSON.stringify(queryKey)}
        queryKey={queryKey}
      />
      <TablePanelComponent
        {...renderer}
        data={linked.data?.data.items.map((item) => ({
          ...item,
          publicId: item.id,
        }))}
        props={{
          keyField: "publicId",
          searchFields: ["providerId"],
          columns: [
            { name: "providerId", labelKey: "admin:security.provider" },
            {
              name: "linkedAt",
              labelKey: "admin:security.linkedAt",
              display: "date",
            },
            {
              name: "revokedAt",
              labelKey: "admin:security.revokedAt",
              display: "date",
            },
          ],
        }}
        renderRowActions={(row) =>
          row.revokedAt ? null : (
            <RowActions
              actions={[
                {
                  label: t("admin:security.unlink"),
                  icon: <Unlink />,
                  danger: true,
                  onClick: () =>
                    setSelected({
                      id: String(row.publicId),
                      provider: String(row.providerId),
                    }),
                },
              ]}
            />
          )
        }
      />
      {selected && (
        <SecurityConfirm
          title={t("admin:security.unlink")}
          description={t("admin:security.unlinkHelp", {
            provider: selected.provider,
          })}
          onClose={() => setSelected(undefined)}
          onConfirm={async (password, key, signal) => {
            await externalIdentityUnlink(
              { id: selected.id, password },
              key,
              signal,
            );
            if (!signal.aborted) await linked.refetch();
          }}
        />
      )}
    </>
  );
}
