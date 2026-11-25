import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Check, LoaderCircle, X } from "lucide-react";
import type { IdentityMembershipListOutput } from "@/api/generated/capability-identity";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  identityMembershipRestore,
  identityMembershipSetExpiry,
  identityMembershipSuspend,
} from "@/lib/capabilities";
import { EditorDialog, ErrorBanner } from "./shared";

const schema = z.object({
  reason: z.string().trim().min(5).max(500),
  expiry: z
    .string()
    .refine((value) => !value || Number.isFinite(Date.parse(value))),
});

function localExpiry(value: string) {
  const date = new Date(value);
  const timestamp = date.getTime();
  if (!Number.isFinite(timestamp) || timestamp <= Date.now()) return "";
  return new Date(timestamp - date.getTimezoneOffset() * 60000)
    .toISOString()
    .slice(0, 16);
}

export function SecurityMembershipChange({
  member,
  operation,
  onClose,
  onChanged,
}: {
  member: IdentityMembershipListOutput["items"][number];
  operation: "suspend" | "restore" | "setExpiry";
  onClose: () => void;
  onChanged: () => void;
}) {
  const { t } = useTranslation();
  const [error, setError] = useState<unknown>();
  const controller = useRef<AbortController | null>(null);
  const inFlight = useRef(false);
  const attempt = useRef<{ args: string; key: string } | undefined>(undefined);
  const form = useForm({
    resolver: zodResolver(schema),
    defaultValues: { reason: "", expiry: localExpiry(member.expiresAt) },
  });
  useEffect(() => {
    const request = new AbortController();
    controller.current = request;
    return () => request.abort();
  }, []);

  const titles = {
    suspend: t("admin:security.workspaceMembers.suspend"),
    restore: t("admin:security.workspaceMembers.restoreMembership"),
    setExpiry: t("admin:security.workspaceMembers.setExpiry"),
  };
  async function submit({ reason, expiry }: z.infer<typeof schema>) {
    if (inFlight.current || !controller.current) return;
    const signal = controller.current.signal;
    const input = { accountId: member.id, reason };
    const expiryInput = {
      ...input,
      expiresAt: expiry ? new Date(expiry).toISOString() : "",
    };
    const args = JSON.stringify(operation === "suspend" ? input : expiryInput);
    if (attempt.current?.args !== args) {
      attempt.current = { args, key: crypto.randomUUID() };
    }
    const key = attempt.current.key;
    inFlight.current = true;
    setError(undefined);
    try {
      if (operation === "suspend")
        await identityMembershipSuspend(input, key, signal);
      else if (operation === "restore")
        await identityMembershipRestore(expiryInput, key, signal);
      else await identityMembershipSetExpiry(expiryInput, key, signal);
      if (!signal.aborted) onChanged();
    } catch (cause) {
      if (!signal.aborted) setError(cause);
    } finally {
      inFlight.current = false;
    }
  }

  return (
    <EditorDialog
      open
      title={titles[operation]}
      description={t("admin:security.workspaceMembers.changeHelp", {
        name: member.username,
      })}
      onOpenChange={(open) => {
        if (!open && !inFlight.current) onClose();
      }}
    >
      <form
        className="entity-form"
        onSubmit={(event) => void form.handleSubmit(submit)(event)}
      >
        {!!error && <ErrorBanner error={error} />}
        {operation !== "suspend" && (
          <label className="form-field">
            <span>{t("admin:security.expires")}</span>
            <Input
              type="datetime-local"
              disabled={form.formState.isSubmitting}
              {...form.register("expiry")}
            />
            <small>{t("admin:security.workspaceMembers.expiryHelp")}</small>
          </label>
        )}
        <label className="form-field">
          <span>{t("admin:policy.reason")}</span>
          <Textarea
            required
            minLength={5}
            maxLength={500}
            disabled={form.formState.isSubmitting}
            aria-invalid={!!form.formState.errors.reason}
            {...form.register("reason")}
          />
          {form.formState.errors.reason && (
            <small>{t("admin:validation.reason")}</small>
          )}
        </label>
        <div className="form-actions">
          <Button
            type="button"
            variant="outline"
            onClick={onClose}
            disabled={form.formState.isSubmitting}
          >
            <X />
            {t("admin:actions.cancel")}
          </Button>
          <Button
            type="submit"
            variant={operation === "suspend" ? "destructive" : "default"}
            disabled={form.formState.isSubmitting}
          >
            {form.formState.isSubmitting ? (
              <LoaderCircle className="animate-spin" />
            ) : (
              <Check />
            )}
            {t("admin:security.confirm")}
          </Button>
        </div>
      </form>
    </EditorDialog>
  );
}
