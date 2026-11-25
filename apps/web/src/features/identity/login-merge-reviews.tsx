import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowDown,
  ArrowLeft,
  ArrowUp,
  Check,
  Link2,
  Undo2,
  X,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { IdentityLoginMergeListOutput } from "@/api/generated/capability-identity";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  identityLoginMergeConfirm,
  identityLoginMergeDecide,
  identityLoginMergeList,
  identityLoginMergeRequest,
} from "@/lib/capabilities";
import { useSession } from "./auth";
import { SecurityConfirm } from "./security-confirm";
import {
  DataTable,
  ErrorBanner,
  Loading,
  RowActions,
  ShortDate,
  type AdminColumn,
} from "./shared";

type Review = IdentityLoginMergeListOutput["items"][number];
type Action =
  | { kind: "request" }
  | { kind: "confirm" | "apply" | "reject" | "rollback"; row: Review };

const labels = {
  request: "admin:loginMerge.request",
  confirm: "admin:loginMerge.confirm",
  apply: "admin:loginMerge.apply",
  reject: "admin:loginMerge.reject",
  rollback: "admin:loginMerge.rollback",
} as const;

const statuses: Record<
  string,
  | "admin:loginMerge.pending"
  | "admin:loginMerge.confirmed"
  | "admin:loginMerge.applied"
  | "admin:loginMerge.rejected"
  | "admin:loginMerge.rolledBack"
> = {
  pending: "admin:loginMerge.pending",
  confirmed: "admin:loginMerge.confirmed",
  applied: "admin:loginMerge.applied",
  rejected: "admin:loginMerge.rejected",
  rolled_back: "admin:loginMerge.rolledBack",
};

export function LoginMergeReviews({
  queryKey,
  onBack,
}: {
  queryKey: readonly unknown[];
  onBack: () => void;
}) {
  const { t } = useTranslation();
  const session = useSession();
  const canReview = session.permissions.includes("access:manage");
  return (
    <div className="space-y-4">
      <Button type="button" variant="outline" onClick={onBack}>
        <ArrowLeft />
        {t("admin:loginAssociation.title")}
      </Button>
      <Tabs defaultValue="mine">
        <TabsList>
          <TabsTrigger value="mine">{t("admin:loginMerge.mine")}</TabsTrigger>
          {canReview && (
            <TabsTrigger value="workspace">
              {t("admin:loginMerge.workspace")}
            </TabsTrigger>
          )}
        </TabsList>
        <TabsContent value="mine">
          <MergeQueue
            queryKey={queryKey}
            reviewer={false}
            canRequest={!session.account.isAdmin && !session.platformOperator}
          />
        </TabsContent>
        {canReview && (
          <TabsContent value="workspace">
            <MergeQueue queryKey={queryKey} reviewer canRequest={false} />
          </TabsContent>
        )}
      </Tabs>
    </div>
  );
}

function MergeQueue({
  queryKey,
  reviewer,
  canRequest,
}: {
  queryKey: readonly unknown[];
  reviewer: boolean;
  canRequest: boolean;
}) {
  const { t } = useTranslation();
  const [before, setBefore] = useState("");
  const [action, setAction] = useState<Action>();
  const result = useQuery({
    queryKey: [...queryKey, "login-merges", reviewer, before],
    queryFn: ({ signal }) =>
      identityLoginMergeList({ reviewer, before: before || undefined }, signal),
    retry: false,
  });
  const columns: AdminColumn<Review>[] = [
    {
      id: "primary",
      header: t("admin:loginMerge.primary"),
      cell: (row) => row.primaryUsername,
    },
    {
      id: "secondary",
      header: t("admin:loginMerge.secondary"),
      cell: (row) => row.secondaryUsername,
    },
    {
      id: "status",
      header: t("admin:fields.status"),
      cell: (row) =>
        statuses[row.status] ? t(statuses[row.status]) : row.status,
    },
    {
      id: "reason",
      header: t("admin:platform.reason"),
      cell: (row) => row.reason,
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
        const kinds: Array<"confirm" | "apply" | "reject" | "rollback"> = [];
        if (!reviewer && row.canConfirm && row.status === "pending")
          kinds.push("confirm");
        if (reviewer && row.status === "confirmed") kinds.push("apply");
        if (
          reviewer &&
          (row.status === "pending" || row.status === "confirmed")
        )
          kinds.push("reject");
        if (reviewer && row.status === "applied") kinds.push("rollback");
        return (
          kinds.length > 0 && (
            <RowActions
              actions={kinds.map((kind) => ({
                label: t(labels[kind]),
                icon:
                  kind === "rollback" ? (
                    <Undo2 />
                  ) : kind === "reject" ? (
                    <X />
                  ) : (
                    <Check />
                  ),
                onClick: () => setAction({ kind, row }),
              }))}
            />
          )
        );
      },
    },
  ];
  return (
    <div className="space-y-3">
      <p className="muted-text">{t("admin:loginMerge.help")}</p>
      {canRequest && (
        <Button type="button" onClick={() => setAction({ kind: "request" })}>
          <Link2 />
          {t(labels.request)}
        </Button>
      )}
      {result.isPending ? (
        <Loading />
      ) : result.error ? (
        <ErrorBanner
          error={result.error}
          onRetry={() => void result.refetch()}
        />
      ) : (
        <DataTable rows={result.data.data.items} columns={columns} serverPage />
      )}
      {(before || result.data?.data.nextBefore) && (
        <div className="form-actions">
          <Button
            type="button"
            variant="outline"
            disabled={!before}
            onClick={() => setBefore("")}
          >
            <ArrowUp />
            {t("admin:security.workspaceMembers.first")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={!result.data?.data.nextBefore}
            onClick={() => setBefore(result.data?.data.nextBefore ?? "")}
          >
            <ArrowDown />
            {t("admin:actions.next")}
          </Button>
        </div>
      )}
      {action && (
        <MergeAction
          action={action}
          onClose={() => setAction(undefined)}
          refresh={() => result.refetch()}
        />
      )}
    </div>
  );
}

function MergeAction({
  action,
  onClose,
  refresh,
}: {
  action: Action;
  onClose: () => void;
  refresh: () => Promise<unknown>;
}) {
  const { t } = useTranslation();
  const [username, setUsername] = useState("");
  const [realm, setRealm] = useState("local");
  const [reason, setReason] = useState("");
  return (
    <SecurityConfirm
      title={t(labels[action.kind])}
      description={t("admin:loginMerge.help")}
      showFactor
      onClose={onClose}
      onConfirm={async (password, key, signal, factor) => {
        const proof = { password, factor: factor || undefined };
        if (action.kind === "request") {
          await identityLoginMergeRequest(
            { ...proof, username, realm, reason },
            key,
            signal,
          );
        } else if (action.kind === "confirm") {
          await identityLoginMergeConfirm(
            { ...proof, id: action.row.id },
            key,
            signal,
          );
        } else {
          await identityLoginMergeDecide(
            { ...proof, id: action.row.id, decision: action.kind, reason },
            key,
            signal,
          );
        }
        if (!signal.aborted) await refresh();
      }}
    >
      {action.kind === "request" && (
        <>
          <label className="form-field">
            <span>{t("admin:loginMerge.secondary")}</span>
            <Input
              required
              minLength={3}
              maxLength={80}
              value={username}
              onChange={(event) => setUsername(event.target.value)}
            />
          </label>
          <label className="form-field">
            <span>{t("admin:login.realm")}</span>
            <Input
              required
              maxLength={63}
              value={realm}
              onChange={(event) => setRealm(event.target.value)}
            />
          </label>
        </>
      )}
      {action.kind !== "confirm" && (
        <label className="form-field">
          <span>{t("admin:platform.reason")}</span>
          <Input
            required
            minLength={5}
            maxLength={500}
            value={reason}
            onChange={(event) => setReason(event.target.value)}
          />
        </label>
      )}
    </SecurityConfirm>
  );
}
