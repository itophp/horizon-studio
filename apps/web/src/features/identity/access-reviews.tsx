import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router";
import {
  ArrowDown,
  CalendarClock,
  Check,
  ListPlus,
  LoaderCircle,
  X,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { IdentityReviewListOutput } from "@/api/generated/capability-identity";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  identityReviewConfigure,
  identityReviewDecide,
  identityReviewList,
  identityReviewStart,
  previewContextRoleRevocation,
} from "@/lib/capabilities";
import {
  DataTable,
  EditorDialog,
  ErrorBanner,
  Loading,
  RowActions,
  ShortDate,
  type AdminColumn,
} from "./shared";

type Review = IdentityReviewListOutput["items"][number];
const reviewStatuses = [
  "pending",
  "retained",
  "revoked",
  "superseded",
] as const;
type Operation =
  | { kind: "schedule" }
  | { kind: "start" }
  | { kind: "decision"; row: Review; decision: "retain" | "revoke" };

export function AccessReviews({ queryKey }: { queryKey: readonly unknown[] }) {
  const { t } = useTranslation();
  const [params, setParams] = useSearchParams();
  const rawAfter = Number(params.get("reviewAfter"));
  const after = Number.isSafeInteger(rawAfter) && rawAfter > 0 ? rawAfter : 0;
  const status = reviewStatuses.find(
    (value) => value === params.get("reviewStatus"),
  );
  const setAfter = (next: number) =>
    setParams((previous) => {
      const updated = new URLSearchParams(previous);
      if (next) updated.set("reviewAfter", String(next));
      else updated.delete("reviewAfter");
      return updated;
    });
  const [operation, setOperation] = useState<Operation>();
  const reviews = useQuery({
    queryKey: [...queryKey, after, status],
    retry: false,
    queryFn: ({ signal }) =>
      identityReviewList({ after: after || undefined, status }, signal),
  });
  const statuses: Record<string, string> = {
    pending: t("admin:review.pending"),
    retained: t("admin:review.retained"),
    revoked: t("admin:review.revoked"),
    superseded: t("admin:review.superseded"),
  };
  const targets: Record<string, string> = {
    membership: t("admin:review.membership"),
    context_role: t("admin:review.role"),
  };
  const columns: AdminColumn<Review>[] = [
    {
      id: "kind",
      header: t("admin:review.target"),
      cell: (row) => targets[row.kind] ?? row.kind,
    },
    {
      id: "role",
      header: t("admin:review.assignment"),
      cell: (row) =>
        row.roleId ? row.roleName || t("admin:review.unavailableName") : null,
    },
    {
      id: "principal",
      header: t("admin:review.principal"),
      cell: (row) => (
        <span>
          {row.principalName || t("admin:review.unavailableName")}
          {row.username && (
            <span className="block muted-text">{row.username}</span>
          )}
        </span>
      ),
    },
    {
      id: "created",
      header: t("admin:review.created"),
      cell: (row) => <ShortDate value={row.createdAt} />,
    },
    {
      id: "status",
      header: t("admin:review.status"),
      cell: (row) => statuses[row.status] ?? row.status,
    },
    {
      id: "actions",
      header: t("admin:fields.actions"),
      cell: (row) =>
        row.status === "pending" ? (
          <RowActions
            actions={[
              {
                label: t("admin:review.retain"),
                icon: <Check />,
                onClick: () =>
                  setOperation({ kind: "decision", row, decision: "retain" }),
              },
              {
                label: t("admin:review.revoke"),
                icon: <X />,
                danger: true,
                onClick: () =>
                  setOperation({ kind: "decision", row, decision: "revoke" }),
              },
            ]}
          />
        ) : (
          row.reason
        ),
    },
  ];
  return (
    <div className="space-y-4">
      <div className="standalone-toolbar">
        <Button type="button" onClick={() => setOperation({ kind: "start" })}>
          <ListPlus />
          {t("admin:review.start")}
        </Button>
        <Button
          type="button"
          variant="outline"
          onClick={() => setOperation({ kind: "schedule" })}
        >
          <CalendarClock />
          {t("admin:review.schedule")}
        </Button>
        <Select
          value={status ?? "all"}
          onValueChange={(value) => {
            setParams((previous) => {
              const updated = new URLSearchParams(previous);
              updated.delete("reviewAfter");
              if (!value || value === "all") updated.delete("reviewStatus");
              else updated.set("reviewStatus", value);
              return updated;
            });
          }}
        >
          <SelectTrigger aria-label={t("admin:review.status")}>
            <SelectValue>
              {status ? statuses[status] : t("admin:review.allStatuses")}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("admin:review.allStatuses")}</SelectItem>
            {reviewStatuses.map((value) => (
              <SelectItem key={value} value={value}>
                {statuses[value]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
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
      <div className="form-actions">
        <Button
          type="button"
          variant="outline"
          disabled={!after || reviews.isFetching}
          onClick={() => setAfter(0)}
        >
          <ListPlus />
          {t("admin:security.workspaceMembers.first")}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={!reviews.data?.data.nextAfter || reviews.isFetching}
          onClick={() => setAfter(reviews.data?.data.nextAfter ?? 0)}
        >
          <ArrowDown />
          {t("admin:actions.next")}
        </Button>
      </div>
      {operation && (
        <ReviewDialog
          operation={operation}
          onClose={() => setOperation(undefined)}
          onSaved={() => {
            setOperation(undefined);
            void reviews.refetch();
          }}
        />
      )}
    </div>
  );
}

function ReviewDialog({
  operation,
  onClose,
  onSaved,
}: {
  operation: Operation;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>();
  const request = useRef<AbortController | null>(null);
  const attempt = useRef<{ value: string; key: string } | undefined>(undefined);
  const previewTarget =
    operation.kind === "decision" &&
    operation.decision === "revoke" &&
    operation.row.kind === "context_role"
      ? operation.row.targetId
      : undefined;
  const preview = useQuery({
    queryKey: ["iam.roleRevokeImpact", previewTarget],
    enabled: !!previewTarget,
    gcTime: 0,
    retry: false,
    queryFn: ({ signal }) =>
      previewContextRoleRevocation(previewTarget ?? "", signal),
  });
  useEffect(() => {
    const controller = new AbortController();
    request.current = controller;
    return () => controller.abort();
  }, []);
  const title =
    operation.kind === "schedule"
      ? t("admin:review.schedule")
      : operation.kind === "start"
        ? t("admin:review.start")
        : t(
            operation.decision === "retain"
              ? "admin:review.retain"
              : "admin:review.revoke",
          );
  return (
    <EditorDialog
      open
      title={title}
      description={t(
        operation.kind === "schedule"
          ? "admin:review.scheduleHelp"
          : operation.kind === "start"
            ? "admin:review.startHelp"
            : "admin:review.decisionHelp",
      )}
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      {previewTarget && preview.isFetching && <Loading />}
      {previewTarget && preview.error && <ErrorBanner error={preview.error} />}
      {preview.data && <p>{t("admin:roleReview.impact", preview.data.data)}</p>}
      <form
        className="entity-form"
        onSubmit={(event) => {
          event.preventDefault();
          const signal = request.current?.signal;
          if (!signal || pending) return;
          const form = new FormData(event.currentTarget);
          const value = String(form.get("value") ?? "").trim();
          if (operation.kind === "decision" && value.length < 5) return;
          if (attempt.current?.value !== value)
            attempt.current = { value, key: crypto.randomUUID() };
          const key = attempt.current.key;
          setPending(true);
          setError(undefined);
          const result =
            operation.kind === "start"
              ? identityReviewStart(key, signal)
              : operation.kind === "schedule"
                ? identityReviewConfigure(
                    { intervalDays: Number(value) },
                    key,
                    signal,
                  )
                : identityReviewDecide(
                    {
                      id: operation.row.id,
                      decision: operation.decision,
                      reason: value,
                    },
                    key,
                    signal,
                  );
          void result
            .then(() => {
              if (!signal.aborted) onSaved();
            })
            .catch((cause: unknown) => {
              if (!signal.aborted) setError(cause);
            })
            .finally(() => {
              if (!signal.aborted) setPending(false);
            });
        }}
      >
        {!!error && <ErrorBanner error={error} />}
        {operation.kind === "schedule" && (
          <label className="form-field">
            <span>{t("admin:review.interval")}</span>
            <Input
              name="value"
              type="number"
              min={1}
              max={365}
              defaultValue={90}
              required
              disabled={pending}
            />
          </label>
        )}
        {operation.kind === "decision" && (
          <label className="form-field">
            <span>{t("admin:policy.reason")}</span>
            <Textarea
              name="value"
              required
              minLength={5}
              maxLength={500}
              disabled={pending}
            />
          </label>
        )}
        <div className="form-actions">
          <Button
            type="button"
            variant="outline"
            onClick={onClose}
            disabled={pending}
          >
            <X />
            {t("admin:actions.cancel")}
          </Button>
          <Button
            type="submit"
            variant={
              operation.kind === "decision" && operation.decision === "revoke"
                ? "destructive"
                : "default"
            }
            disabled={pending}
          >
            {pending ? <LoaderCircle className="animate-spin" /> : <Check />}
            {t("admin:security.confirm")}
          </Button>
        </div>
      </form>
    </EditorDialog>
  );
}
