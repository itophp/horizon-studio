import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { IdentityAgentDelegationListOutput } from "@/api/generated/capability-identity";
import { Button } from "@/components/ui/button";
import { agentDelegationList, agentDelegationRevoke } from "@/lib/capabilities";
import { SecurityConfirm } from "./security-confirm";
import {
  DataTable,
  ErrorBanner,
  Loading,
  RowActions,
  ShortDate,
  type AdminColumn,
} from "./shared";

type Delegation = IdentityAgentDelegationListOutput["items"][number];

export function AgentDelegations({
  queryKey,
}: {
  queryKey: readonly unknown[];
}) {
  const { t } = useTranslation();
  const [after, setAfter] = useState("");
  const [selected, setSelected] = useState<Delegation>();
  const grants = useQuery({
    queryKey: [...queryKey, "agent-delegations", after],
    queryFn: ({ signal }) =>
      agentDelegationList(after ? { after } : {}, signal),
    retry: false,
  });
  const columns: AdminColumn<Delegation>[] = [
    {
      id: "agent",
      header: t("admin:delegations.agent"),
      cell: (row) => row.agentAccountId,
    },
    {
      id: "capability",
      header: t("admin:machine.capabilities"),
      cell: (row) => [row.capability, ...row.additionalCapabilities].join(", "),
    },
    {
      id: "resource",
      header: t("admin:delegations.resource"),
      cell: (row) => row.resourceId,
    },
    {
      id: "remaining",
      header: t("admin:delegations.remaining"),
      cell: (row) => row.remainingUses,
    },
    {
      id: "expires",
      header: t("admin:security.expires"),
      cell: (row) => <ShortDate value={row.expiresAt} />,
    },
    {
      id: "actions",
      header: t("admin:fields.actions"),
      cell: (row) =>
        row.revokedAt ? (
          <ShortDate value={row.revokedAt} />
        ) : (
          <RowActions
            actions={[
              {
                label: t("admin:delegations.revoke"),
                icon: <X />,
                danger: true,
                onClick: () => setSelected(row),
              },
            ]}
          />
        ),
    },
  ];
  if (grants.isPending) return <Loading />;
  if (grants.error)
    return (
      <ErrorBanner error={grants.error} onRetry={() => void grants.refetch()} />
    );
  const { items, nextAfter } = grants.data.data;
  return (
    <div className="space-y-4">
      <DataTable rows={items} columns={columns} serverPage />
      {(after || nextAfter) && (
        <div className="form-actions">
          <Button
            type="button"
            variant="outline"
            disabled={!after}
            onClick={() => setAfter("")}
          >
            <ArrowUp />
            {t("admin:security.workspaceMembers.first")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={!nextAfter}
            onClick={() => setAfter(nextAfter)}
          >
            <ArrowDown />
            {t("admin:actions.next")}
          </Button>
        </div>
      )}
      {selected && (
        <SecurityConfirm
          title={t("admin:delegations.revoke")}
          description={t("admin:delegations.revokeHelp", {
            capability: selected.capability,
          })}
          onClose={() => setSelected(undefined)}
          onConfirm={async (password, key, signal) => {
            await agentDelegationRevoke(
              { id: selected.id, password },
              key,
              signal,
            );
            if (!signal.aborted) void grants.refetch();
          }}
        />
      )}
    </div>
  );
}
