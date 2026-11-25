import { useEffect, useRef, useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Check, ListChecks, LoaderCircle, X } from "lucide-react";
import type { IdentityPolicyCheckOutput } from "@/api/generated/capability-identity";
import {
  identityPolicyCheck,
  identityPolicyDraft,
  identityPolicyPreview,
  identityPolicyRollback,
} from "@/lib/capabilities";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { EditorDialog, ErrorBanner, Loading } from "./shared";
import { PolicyIssues } from "./policy-issues";

const name = z.string().regex(/^[a-zA-Z][a-zA-Z0-9_.-]{0,199}$/);
const rulesSchema = z
  .array(
    z
      .object({
        action: name,
        resource: name,
        effect: z.enum([
          "deny",
          "require_binding",
          "require_attribute",
          "filter_row",
          "mask_output",
        ]),
        bindingType: name.optional(),
        attribute: name.optional(),
        equals: z.string().min(1).max(256).optional(),
      })
      .strict()
      .refine((rule) => rule.effect !== "require_binding" || !!rule.bindingType)
      .refine(
        (rule) =>
          rule.effect !== "mask_output" || (!!rule.attribute && !rule.equals),
      )
      .refine(
        (rule) =>
          (rule.effect !== "require_attribute" &&
            rule.effect !== "filter_row") ||
          (!!rule.attribute && !!rule.equals),
      ),
  )
  .max(100);
function parsedRules(value: string) {
  try {
    return rulesSchema.safeParse(JSON.parse(value));
  } catch {
    return undefined;
  }
}
const draftSchema = z.object({
  reason: z.string().trim().min(5).max(500),
  rules: z
    .string()
    .max(100000)
    .refine((value) => parsedRules(value)?.success === true),
});

export function PolicyDraftDialog({
  baseVersion,
  rollbackId,
  queryKey,
  onClose,
  onSaved,
}: {
  baseVersion: number;
  rollbackId?: number;
  queryKey: readonly unknown[];
  onClose: () => void;
  onSaved: (id: number) => void;
}) {
  const { t } = useTranslation();
  const [busy, setBusy] = useState(false);
  // Background history refreshes must not silently rebase an open draft.
  const [originalBase] = useState(baseVersion);
  const source = rollbackId ?? originalBase;
  const preview = useQuery({
    queryKey: [...queryKey, "preview", source],
    enabled: source > 0,
    queryFn: ({ signal }) => identityPolicyPreview({ id: source }, signal),
    retry: false,
  });
  return (
    <EditorDialog
      open
      wide
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
      title={t(rollbackId ? "admin:policy.rollback" : "admin:policy.create")}
      description={t(
        rollbackId ? "admin:policy.rollbackHelp" : "admin:policy.createHelp",
      )}
    >
      {source > 0 && preview.isPending ? (
        <Loading />
      ) : preview.error ? (
        <ErrorBanner
          error={preview.error}
          onRetry={() => void preview.refetch()}
        />
      ) : (
        <DraftForm
          baseVersion={originalBase}
          rollbackId={rollbackId}
          rules={preview.data?.data.proposedRules ?? []}
          onClose={onClose}
          onSaved={onSaved}
          onBusy={setBusy}
        />
      )}
    </EditorDialog>
  );
}

function DraftForm({
  baseVersion,
  rollbackId,
  rules,
  onClose,
  onSaved,
  onBusy,
}: {
  baseVersion: number;
  rollbackId?: number;
  rules: z.infer<typeof rulesSchema>;
  onClose: () => void;
  onSaved: (id: number) => void;
  onBusy: (value: boolean) => void;
}) {
  const { t } = useTranslation();
  const request = useRef({ payload: "", key: crypto.randomUUID() });
  const active = useRef(true);
  const checkRequest = useRef<AbortController>(null);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
      checkRequest.current?.abort();
    };
  }, []);
  const [error, setError] = useState<unknown>();
  const [check, setCheck] = useState<{
    source: string;
    result: IdentityPolicyCheckOutput;
  }>();
  const [checking, setChecking] = useState(false);
  const submitting = useRef(false);
  const form = useForm({
    resolver: zodResolver(draftSchema),
    defaultValues: { reason: "", rules: JSON.stringify(rules, null, 2) },
  });
  const source = useWatch({ control: form.control, name: "rules" });
  async function checkRules() {
    const parsed = parsedRules(source);
    if (!parsed?.success) {
      await form.trigger("rules");
      return;
    }
    const controller = new AbortController();
    checkRequest.current?.abort();
    checkRequest.current = controller;
    setChecking(true);
    setError(undefined);
    try {
      const response = await identityPolicyCheck(
        { rules: parsed.data },
        controller.signal,
      );
      if (!controller.signal.aborted)
        setCheck({ source, result: response.data });
    } catch (cause) {
      if (!controller.signal.aborted) setError(cause);
    } finally {
      if (!controller.signal.aborted) setChecking(false);
    }
  }
  return (
    <form
      className="entity-form"
      onSubmit={(event) => {
        void form.handleSubmit(async (values) => {
          if (submitting.current) return;
          submitting.current = true;
          setError(undefined);
          onBusy(true);
          try {
            const input = { baseVersion, reason: values.reason };
            const payload = JSON.stringify({
              ...input,
              rollbackId,
              rules: values.rules,
            });
            if (request.current.payload !== payload)
              request.current = { payload, key: crypto.randomUUID() };
            const result = rollbackId
              ? await identityPolicyRollback(
                  { ...input, id: rollbackId },
                  request.current.key,
                )
              : await identityPolicyDraft(
                  {
                    ...input,
                    rules: rulesSchema.parse(JSON.parse(values.rules)),
                  },
                  request.current.key,
                );
            if (active.current && result.data.id) onSaved(result.data.id);
          } catch (cause) {
            if (active.current) setError(cause);
          } finally {
            submitting.current = false;
            if (active.current) onBusy(false);
          }
        })(event);
      }}
    >
      {!!error && <ErrorBanner error={error} />}
      <label className="form-field">
        <span id="policy-reason-label">{t("admin:policy.reason")}</span>
        <Textarea
          required
          minLength={5}
          maxLength={500}
          aria-labelledby="policy-reason-label"
          aria-describedby="policy-reason-help"
          aria-invalid={!!form.formState.errors.reason}
          {...form.register("reason")}
        />
        <small id="policy-reason-help">
          {t(
            form.formState.errors.reason
              ? "admin:validation.reason"
              : "admin:policy.reasonHelp",
          )}
        </small>
      </label>
      <label className="form-field">
        <span id="policy-rules-label">{t("admin:policy.rules")}</span>
        <Textarea
          className="font-mono min-h-56"
          readOnly={!!rollbackId}
          aria-labelledby="policy-rules-label"
          aria-describedby="policy-rules-help"
          aria-invalid={!!form.formState.errors.rules}
          {...form.register("rules")}
        />
        <small id="policy-rules-help">
          {t(
            form.formState.errors.rules
              ? "admin:policy.invalidRules"
              : "admin:policy.rulesHelp",
          )}
        </small>
      </label>
      {check?.source === source && (
        <PolicyIssues issues={check.result.issues} />
      )}
      <div className="form-actions">
        {!rollbackId && (
          <Button
            type="button"
            variant="outline"
            disabled={checking || form.formState.isSubmitting}
            onClick={() => void checkRules()}
          >
            {checking ? (
              <LoaderCircle className="animate-spin" />
            ) : (
              <ListChecks />
            )}
            {t("admin:policy.check")}
          </Button>
        )}
        <Button
          type="button"
          variant="outline"
          disabled={form.formState.isSubmitting}
          onClick={onClose}
        >
          <X />
          {t("admin:actions.cancel")}
        </Button>
        <Button type="submit" disabled={form.formState.isSubmitting}>
          {form.formState.isSubmitting ? (
            <LoaderCircle className="animate-spin" />
          ) : (
            <Check />
          )}
          {t("admin:policy.save")}
        </Button>
      </div>
    </form>
  );
}
