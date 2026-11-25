import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, RotateCcw, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { IdentityExternalReviewListOutput } from "@/api/generated/capability-identity";
import { Button } from "@/components/ui/button";
import {
  identityExternalReviewConfirm,
  identityExternalReviewDecide,
  identityExternalReviewList,
} from "@/lib/capabilities";
import { useSession } from "./auth";
import { IAMDecisionDialog } from "./iam-decision-dialog";
import {
  DataTable,
  ErrorBanner,
  Loading,
  RowActions,
  ShortDate,
  type AdminColumn,
} from "./shared";

type Review = IdentityExternalReviewListOutput["items"][number];
type Action = "confirm" | "apply" | "reject" | "rollback";

export function ExternalIdentityReviews({
  queryKey,
}: {
  queryKey: readonly unknown[];
}) {
  const { t } = useTranslation();
  const session = useSession();
  const [reviewer, setReviewer] = useState(false);
  const [after, setAfter] = useState("");
  const [operation, setOperation] = useState<{ id: string; action: Action }>();
  const canReview = session.permissions.includes("access:manage");
  const reviews = useQuery({
    queryKey: [...queryKey, "external-reviews", reviewer, after],
    enabled: !session.identityBindingId,
    retry: false,
    queryFn: ({ signal }) =>
      identityExternalReviewList(
        { reviewer, ...(after ? { after } : {}) },
        signal,
      ),
  });
  if (session.identityBindingId) return null;
  const labels = {
    confirm: t("admin:externalReview.confirm"),
    apply: t("admin:externalReview.apply"),
    reject: t("admin:externalReview.reject"),
    rollback: t("admin:externalReview.rollback"),
  };
  const statuses: Record<string, string> = {
    pending: t("admin:review.pending"),
    confirmed: t("admin:externalReview.confirmed"),
    applied: t("admin:externalReview.applied"),
    rejected: t("admin:externalReview.rejected"),
    rolled_back: t("admin:externalReview.rolledBack"),
    expired: t("admin:externalReview.expired"),
  };
  const columns: AdminColumn<Review>[] = [
    {
      id: "provider",
      header: t("admin:security.provider"),
      cell: (row) => row.providerId,
    },
    {
      id: "status",
      header: t("admin:review.status"),
      cell: (row) => statuses[row.status] ?? row.status,
    },
    {
      id: "expires",
      header: t("admin:security.expires"),
      cell: (row) => <ShortDate value={row.expiresAt} />,
    },
    {
      id: "actions",
      header: t("admin:fields.actions"),
      cell: (row) => {
        const actions: Action[] = [];
        if (row.status === "pending" && row.originalOwner)
          actions.push("confirm");
        if (reviewer && !row.originalOwner && !row.requestedOwner) {
          if (row.status === "confirmed") actions.push("apply");
          if (row.status === "pending" || row.status === "confirmed")
            actions.push("reject");
          if (row.status === "applied") actions.push("rollback");
        }
        return actions.length ? (
          <RowActions
            actions={actions.map((action) => ({
              label: labels[action],
              icon:
                action === "rollback" ? (
                  <RotateCcw />
                ) : action === "reject" ? (
                  <X />
                ) : (
                  <Check />
                ),
              danger: action === "reject" || action === "rollback",
              onClick: () => setOperation({ id: row.id, action }),
            }))}
          />
        ) : (
          row.reason
        );
      },
    },
  ];
  return (
    <section className="space-y-4" aria-label={t("admin:externalReview.title")}>
      <div className="standalone-toolbar">
        <h2>{t("admin:externalReview.title")}</h2>
        {canReview && (
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              setReviewer(!reviewer);
              setAfter("");
            }}
          >
            {reviewer
              ? t("admin:externalReview.mine")
              : t("admin:externalReview.workspace")}
          </Button>
        )}
      </div>
      {reviews.isPending ? (
        <Loading />
      ) : reviews.error ? (
        <ErrorBanner
          error={reviews.error}
          onRetry={() => void reviews.refetch()}
        />
      ) : (
        <DataTable
          rows={reviews.data.data.items}
          columns={columns}
          serverPage
        />
      )}
      {(after || reviews.data?.data.nextAfter) && (
        <div className="form-actions">
          <Button
            type="button"
            variant="outline"
            disabled={!after}
            onClick={() => setAfter("")}
          >
            {t("admin:security.workspaceMembers.first")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={!reviews.data?.data.nextAfter}
            onClick={() => setAfter(reviews.data?.data.nextAfter ?? "")}
          >
            {t("admin:actions.next")}
          </Button>
        </div>
      )}
      {operation && (
        <IAMDecisionDialog
          title={labels[operation.action]}
          requireReason={operation.action !== "confirm"}
          description={t(
            operation.action === "confirm"
              ? "admin:externalReview.confirmHelp"
              : "admin:externalReview.decisionHelp",
          )}
          onConfirm={(reason, key, signal) =>
            operation.action === "confirm"
              ? identityExternalReviewConfirm({ id: operation.id }, key, signal)
              : identityExternalReviewDecide(
                  { id: operation.id, decision: operation.action, reason },
                  key,
                  signal,
                )
          }
          onClose={() => setOperation(undefined)}
          onSaved={() => {
            setOperation(undefined);
            void reviews.refetch();
          }}
        />
      )}
    </section>
  );
}
