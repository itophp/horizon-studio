import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Check, LoaderCircle, X } from "lucide-react";
import type { IdentityMembershipListOutput } from "@/api/generated/capability-identity";
import {
  identityLoginFreeze,
  identityLoginReauthenticate,
  identityLoginRestore,
} from "@/lib/capabilities";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { EditorDialog, ErrorBanner } from "./shared";

const schema = z.object({
  password: z.string().min(1).max(128),
  reason: z.string().trim().min(5).max(500),
});

export function SecurityLoginStatus({
  member,
  onClose,
  onChanged,
}: {
  member: IdentityMembershipListOutput["items"][number];
  onClose: () => void;
  onChanged: () => void;
}) {
  const { t } = useTranslation();
  const operation = member.loginStatus === "frozen" ? "restore" : "freeze";
  const submitting = useRef(false);
  const controller = useRef<AbortController | null>(null);
  const key = useRef(crypto.randomUUID());
  const [error, setError] = useState<unknown>();
  useEffect(() => {
    const request = new AbortController();
    controller.current = request;
    return () => request.abort();
  }, []);
  const form = useForm({
    resolver: zodResolver(schema),
    defaultValues: { password: "", reason: "" },
  });
  return (
    <EditorDialog
      open
      title={t(
        operation === "freeze"
          ? "admin:security.workspaceMembers.freeze"
          : "admin:security.workspaceMembers.restore",
      )}
      description={t("admin:security.workspaceMembers.loginChangeHelp", {
        name: member.username,
        login: member.username,
      })}
      onOpenChange={(open) => {
        if (!open && !submitting.current) onClose();
      }}
    >
      <form
        className="entity-form"
        onSubmit={(event) => {
          void form.handleSubmit(async ({ password, reason }) => {
            if (submitting.current) return;
            submitting.current = true;
            setError(undefined);
            const signal = controller.current!.signal;
            try {
              const proof = await identityLoginReauthenticate(
                { accountId: member.id, operation, password },
                key.current + ":proof",
                signal,
              );
              if (signal.aborted) return;
              const input = {
                accountId: member.id,
                proof: proof.data.proof,
                reason,
              };
              if (operation === "freeze")
                await identityLoginFreeze(
                  input,
                  key.current + ":change",
                  signal,
                );
              else
                await identityLoginRestore(
                  input,
                  key.current + ":change",
                  signal,
                );
              if (!signal.aborted) {
                form.reset();
                onChanged();
              }
            } catch (cause) {
              if (!signal.aborted) {
                setError(cause);
                key.current = crypto.randomUUID();
                form.resetField("password");
              }
            } finally {
              submitting.current = false;
            }
          })(event);
        }}
      >
        {!!error && <ErrorBanner error={error} />}
        <label className="form-field">
          <span>{t("admin:login.password")}</span>
          <Input
            type="password"
            required
            autoComplete="current-password"
            maxLength={128}
            disabled={form.formState.isSubmitting}
            {...form.register("password")}
          />
        </label>
        <label className="form-field">
          <span id="login-change-reason-label">{t("admin:policy.reason")}</span>
          <Textarea
            required
            minLength={5}
            maxLength={500}
            disabled={form.formState.isSubmitting}
            aria-labelledby="login-change-reason-label"
            aria-describedby="login-change-reason-help"
            aria-invalid={!!form.formState.errors.reason}
            {...form.register("reason")}
          />
          <small id="login-change-reason-help">
            {t(
              form.formState.errors.reason
                ? "admin:validation.reason"
                : "admin:policy.reasonHelp",
            )}
          </small>
        </label>
        <div className="form-actions">
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
            {t("admin:security.confirm")}
          </Button>
        </div>
      </form>
    </EditorDialog>
  );
}
