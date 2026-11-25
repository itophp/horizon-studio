import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { ArrowLeft, Check, RefreshCw, RotateCcw, X } from "lucide-react";
import type { IdentityPolicyPreviewOutput } from "@/api/generated/capability-identity";
import {
  identityPolicyPreview,
  identityPolicyPublish,
} from "@/lib/capabilities";
import { Button } from "@/components/ui/button";
import {
  EditorDialog,
  ErrorBanner,
  Loading,
  ShortDate,
  SuccessNotice,
} from "./shared";
import { PolicyDraftDialog } from "./policy-draft";
import { PolicySimulation } from "./policy-simulation";
import { PolicyIssues } from "./policy-issues";

type Rule = IdentityPolicyPreviewOutput["proposedRules"][number];
function Rules({ title, rules }: { title: string; rules: Rule[] }) {
  const { t } = useTranslation();
  return (
    <section className="space-y-2">
      <h2 className="font-medium">{title}</h2>
      {rules.length ? (
        <pre className="admin-panel overflow-x-auto whitespace-pre-wrap break-all p-4 text-xs">
          {JSON.stringify(rules, null, 2)}
        </pre>
      ) : (
        <p className="muted-text">{t("admin:policy.noRules")}</p>
      )}
    </section>
  );
}

export function PolicyDetail({
  id,
  queryKey,
  onBack,
  onChanged,
}: {
  id: number;
  queryKey: readonly unknown[];
  onBack: () => void;
  onChanged: () => void;
}) {
  const { t } = useTranslation();
  const detail = useQuery({
    queryKey: [...queryKey, "preview", id],
    queryFn: ({ signal }) => identityPolicyPreview({ id }, signal),
    retry: false,
  });
  const [confirm, setConfirm] = useState(false);
  const [rollback, setRollback] = useState(false);
  const key = useRef(crypto.randomUUID());
  const active = useRef(true);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
    };
  }, []);
  const publish = useMutation({
    mutationFn: () => identityPolicyPublish({ id }, key.current),
    retry: false,
    onSuccess: () => {
      if (active.current) {
        setConfirm(false);
        void detail.refetch();
        onChanged();
      }
    },
  });
  return (
    <div className="space-y-4">
      <div className="standalone-toolbar flex-wrap">
        <Button type="button" variant="outline" onClick={onBack}>
          <ArrowLeft />
          {t("admin:policy.back")}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={detail.isFetching}
          onClick={() => void detail.refetch()}
        >
          <RefreshCw />
          {t("admin:actions.refresh")}
        </Button>
      </div>
      {publish.isSuccess && (
        <SuccessNotice
          message={t("admin:policy.publishSuccess")}
          onClose={() => publish.reset()}
        />
      )}
      {detail.isPending ? (
        <Loading />
      ) : detail.error ? (
        <ErrorBanner
          error={detail.error}
          onRetry={() => void detail.refetch()}
        />
      ) : (
        (() => {
          const policy = detail.data.data;
          return (
            <>
              <h2 className="font-medium">
                {t("admin:policy.versionLabel", { version: id })}
              </h2>
              <p>{policy.reason}</p>
              {policy.createdAt && (
                <dl className="grid gap-3 sm:grid-cols-2">
                  <div>
                    <dt className="muted-text">{t("admin:policy.author")}</dt>
                    <dd>
                      {policy.authorName || t("admin:review.unavailableName")}{" "}
                      <ShortDate value={policy.createdAt} />
                    </dd>
                  </div>
                  {policy.publishedAt && (
                    <div>
                      <dt className="muted-text">
                        {t("admin:policy.reviewer")}
                      </dt>
                      <dd>
                        {policy.reviewerName ||
                          t("admin:review.unavailableName")}{" "}
                        <ShortDate value={policy.publishedAt} />
                      </dd>
                    </div>
                  )}
                </dl>
              )}
              {policy.published ? (
                <p className="muted-text">
                  {t(
                    id === policy.currentVersion
                      ? "admin:policy.current"
                      : "admin:policy.published",
                  )}
                </p>
              ) : policy.stale ? (
                <p role="status">{t("admin:policy.stale")}</p>
              ) : policy.authoredByMe ? (
                <p className="muted-text">{t("admin:policy.selfApproval")}</p>
              ) : null}
              <div className="standalone-toolbar flex-wrap">
                {!policy.published && (
                  <Button
                    type="button"
                    disabled={
                      policy.stale ||
                      policy.authoredByMe ||
                      detail.isFetching ||
                      publish.isPending
                    }
                    onClick={() => setConfirm(true)}
                  >
                    <Check />
                    {t("admin:policy.publish")}
                  </Button>
                )}
                {policy.published && (
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setRollback(true)}
                  >
                    <RotateCcw />
                    {t("admin:policy.rollback")}
                  </Button>
                )}
              </div>
              <Rules
                title={t("admin:policy.additions")}
                rules={policy.additions}
              />
              <Rules
                title={t("admin:policy.removals")}
                rules={policy.removals}
              />
              <Rules
                title={t("admin:policy.rules")}
                rules={policy.proposedRules}
              />
              {!!policy.issues?.length && (
                <PolicyIssues issues={policy.issues} />
              )}
              <PolicySimulation
                key={JSON.stringify([id, policy.currentVersion])}
                id={id}
                queryKey={queryKey}
                rules={[
                  ...new Map(
                    [...policy.proposedRules, ...policy.removals].map(
                      (rule) => [
                        JSON.stringify([rule.action, rule.resource]),
                        rule,
                      ],
                    ),
                  ).values(),
                ]}
              />
              {confirm && (
                <EditorDialog
                  open
                  title={t("admin:policy.publish")}
                  description={t("admin:policy.publishHelp")}
                  onOpenChange={(open) => {
                    if (!open && !publish.isPending) setConfirm(false);
                  }}
                >
                  {publish.error && <ErrorBanner error={publish.error} />}
                  <div className="form-actions">
                    <Button
                      type="button"
                      variant="outline"
                      disabled={publish.isPending}
                      onClick={() => setConfirm(false)}
                    >
                      <X />
                      {t("admin:actions.cancel")}
                    </Button>
                    <Button
                      type="button"
                      disabled={
                        publish.isPending ||
                        policy.stale ||
                        policy.authoredByMe ||
                        policy.published
                      }
                      onClick={() => publish.mutate()}
                    >
                      <Check />
                      {t("admin:policy.confirm")}
                    </Button>
                  </div>
                </EditorDialog>
              )}
              {rollback && (
                <PolicyDraftDialog
                  baseVersion={policy.currentVersion}
                  rollbackId={id}
                  queryKey={queryKey}
                  onClose={() => setRollback(false)}
                  onSaved={() => {
                    setRollback(false);
                    onChanged();
                    onBack();
                  }}
                />
              )}
            </>
          );
        })()
      )}
    </div>
  );
}
