import { useEffect, useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Play } from "lucide-react";
import type { IdentityPolicyPreviewOutput } from "@/api/generated/capability-identity";
import { identityPolicySimulate } from "@/lib/capabilities";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useSession } from "./auth";
import { ErrorBanner } from "./shared";
import {
  PolicyContextSelect,
  type PolicyContext,
} from "./policy-context-select";

export function PolicySimulation({
  id,
  rules,
  queryKey,
}: {
  id: number;
  rules: IdentityPolicyPreviewOutput["proposedRules"];
  queryKey: readonly unknown[];
}) {
  const { t } = useTranslation();
  const session = useSession();
  const [index, setIndex] = useState(0);
  const [subject, setSubject] = useState<PolicyContext | null>(null);
  const pending = useRef(new Set<AbortController>());
  const selection = JSON.stringify([id, index, subject?.accountId]);
  useEffect(() => {
    const controllers = pending.current;
    return () => {
      for (const controller of controllers) controller.abort();
      controllers.clear();
    };
  }, [selection]);
  const simulation = useMutation({
    mutationFn: async () => {
      const rule = rules[index];
      const controller = new AbortController();
      pending.current.add(controller);
      try {
        const result = await identityPolicySimulate(
          {
            id,
            accountId: subject?.accountId ?? session.account.id,
            bindingId: subject
              ? undefined
              : session.identityBindingId || undefined,
            action: rule.action,
            resource: rule.resource,
          },
          controller.signal,
        );
        if (controller.signal.aborted) return undefined;
        return { result: result.data, selection };
      } finally {
        pending.current.delete(controller);
      }
    },
    retry: false,
  });
  if (!rules.length) return null;
  const result =
    simulation.data?.selection === selection
      ? simulation.data.result
      : undefined;
  return (
    <section className="space-y-3">
      <h2 className="font-medium">{t("admin:policy.simulate")}</h2>
      <p className="muted-text">{t("admin:policy.simulationHelp")}</p>
      <div className="standalone-toolbar flex-wrap">
        <Select
          value={String(index)}
          onValueChange={(value) => {
            setIndex(Number(value));
            simulation.reset();
          }}
        >
          <SelectTrigger aria-label={t("admin:policy.rule")}>
            <SelectValue>{rules[index]?.action}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            {rules.map((rule, i) => (
              <SelectItem key={i} value={String(i)}>
                {t("admin:policy.ruleLabel", {
                  action: rule.action,
                  resource: rule.resource,
                })}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <PolicyContextSelect
          queryKey={queryKey}
          value={subject}
          onChange={(value) => {
            setSubject(value);
            simulation.reset();
          }}
        />
        <Button
          type="button"
          variant="outline"
          disabled={!rules[index] || simulation.isPending}
          onClick={() => simulation.mutate()}
        >
          <Play />
          {t("admin:policy.simulate")}
        </Button>
      </div>
      {simulation.error && <ErrorBanner error={simulation.error} />}
      {result && (
        <dl className="admin-panel space-y-2 p-4" aria-live="polite">
          {[
            {
              title: t("admin:policy.before"),
              allowed: result.currentAllowed,
              reason: result.currentReason,
              constraints: result.currentConstraints,
            },
            {
              title: t("admin:policy.after"),
              allowed: result.proposedAllowed,
              reason: result.proposedReason,
              constraints: result.proposedConstraints,
            },
          ].map(({ title, allowed, reason, constraints }) => (
            <div key={title}>
              <dt>{title}</dt>
              <dd>
                {t("admin:policy.decision", {
                  decision: t(
                    allowed ? "admin:policy.allowed" : "admin:policy.denied",
                  ),
                  reason,
                })}
              </dd>
              {constraints?.requiresBinding && (
                <dd>{t("admin:policy.requiredBinding")}</dd>
              )}
              {!!constraints?.rowPredicates.length && (
                <dd>
                  {t("admin:policy.filteredRows", {
                    filters: constraints.rowPredicates
                      .map(
                        ({ attribute, equals }) => attribute + " = " + equals,
                      )
                      .join(", "),
                  })}
                </dd>
              )}
              {!!constraints?.maskedFields.length && (
                <dd>
                  {t("admin:policy.maskedFields", {
                    fields: constraints.maskedFields.join(", "),
                  })}
                </dd>
              )}
            </div>
          ))}
        </dl>
      )}
    </section>
  );
}
