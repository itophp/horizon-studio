import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, Check, Pause, Unlink } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { IdentityBindingListOutput } from "@/api/generated/capability-identity";
import { Button } from "@/components/ui/button";
import { identityBindingChange, identityBindingList } from "@/lib/capabilities";
import { IAMDecisionDialog } from "./iam-decision-dialog";
import {
  DataTable,
  ErrorBanner,
  Loading,
  RowActions,
  ShortDate,
  type AdminColumn,
} from "./shared";

type Binding = IdentityBindingListOutput["items"][number];
type Action = "approve" | "suspend" | "remove";

export function IdentityBindings({
  queryKey,
}: {
  queryKey: readonly unknown[];
}) {
  const { t } = useTranslation();
  const [after, setAfter] = useState("");
  const [selected, setSelected] = useState<{ id: string; action: Action }>();
  const bindings = useQuery({
    queryKey: [...queryKey, after],
    queryFn: ({ signal }) =>
      identityBindingList(after ? { after } : {}, signal),
    retry: false,
  });
  const actions = {
    approve: { label: t("admin:bindings.approve"), icon: <Check /> },
    suspend: { label: t("admin:bindings.suspend"), icon: <Pause /> },
    remove: { label: t("admin:bindings.remove"), icon: <Unlink /> },
  };
  const statuses: Record<string, string> = {
    pending: t("admin:review.pending"),
    active: t("admin:status.active"),
    suspended: t("admin:bindings.suspended"),
    expired: t("admin:externalReview.expired"),
    revoked: t("admin:review.revoked"),
  };
  const columns: AdminColumn<Binding>[] = [
    {
      id: "identity",
      header: t("entity:identity_binding.label"),
      cell: (row) => `${row.identityType}@${row.identityVersion}`,
    },
    {
      id: "entity",
      header: t("admin:bindings.reference"),
      cell: (row) => row.entityId,
    },
    {
      id: "principal",
      header: t("admin:review.principal"),
      cell: (row) => row.principalId,
    },
    {
      id: "verification",
      header: t("admin:bindings.verification"),
      cell: (row) => row.verificationLevel,
    },
    {
      id: "expires",
      header: t("admin:security.expires"),
      cell: (row) => <ShortDate value={row.validUntil} />,
    },
    {
      id: "status",
      header: t("admin:review.status"),
      cell: (row) => statuses[row.status] ?? row.status,
    },
    {
      id: "actions",
      header: t("admin:fields.actions"),
      cell: (row) => {
        if (row.status === "revoked") return null;
        const allowed: Action[] = ["remove"];
        if (row.status === "active") allowed.unshift("suspend");
        if (row.status === "pending") allowed.unshift("approve");
        return (
          <RowActions
            actions={allowed.map((action) => ({
              ...actions[action],
              danger: action === "remove",
              onClick: () => setSelected({ id: row.id, action }),
            }))}
          />
        );
      },
    },
  ];
  if (bindings.isPending) return <Loading />;
  if (bindings.error)
    return (
      <ErrorBanner
        error={bindings.error}
        onRetry={() => void bindings.refetch()}
      />
    );
  const { items, nextAfter } = bindings.data.data;
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
        <IAMDecisionDialog
          title={actions[selected.action].label}
          description={t("admin:bindings.reasonHelp")}
          onConfirm={(reason, key, signal) =>
            identityBindingChange(
              selected.action,
              { id: selected.id, reason },
              key,
              signal,
            )
          }
          onClose={() => setSelected(undefined)}
          onSaved={() => {
            setSelected(undefined);
            void bindings.refetch();
          }}
        />
      )}
    </div>
  );
}
