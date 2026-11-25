import { useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useTranslation } from "react-i18next";
import { Check, LoaderCircle, Send, X } from "lucide-react";
import type { IdentityMembershipInviteOutput } from "@/api/generated/capability-identity";
import { identityMembershipInvite } from "@/lib/capabilities";
import { formatDate } from "@/lib/format";
import { useConfiguration } from "@/features/governance/runtime";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { EditorDialog, ErrorBanner } from "./shared";

const schema = z.object({
  realm: z
    .string()
    .trim()
    .regex(/^(?:[a-z0-9][a-z0-9_-]{0,62})?$/),
  username: z
    .string()
    .trim()
    .regex(/^[A-Za-z0-9_.@-]{3,80}$/),
});

export function SecurityInvitation({ onClose }: { onClose: () => void }) {
  const { t, i18n } = useTranslation();
  const config = useConfiguration();
  const [result, setResult] = useState<IdentityMembershipInviteOutput>();
  const [error, setError] = useState<unknown>();
  const busy = useRef(false);
  const active = useRef(true);
  const request = useRef({ payload: "", key: crypto.randomUUID() });
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
    };
  }, []);
  const form = useForm({
    resolver: zodResolver(schema),
    defaultValues: { username: "", realm: "" },
  });
  return (
    <EditorDialog
      open
      title={t("admin:security.memberships.invite")}
      description={t("admin:security.memberships.inviteHelp")}
      onOpenChange={(open) => {
        if (!open && !busy.current) onClose();
      }}
    >
      {result ? (
        <div className="space-y-4">
          <p>
            {t("admin:security.memberships.discloseOnce", {
              expires: formatDate(
                result.expiresAt,
                i18n.language,
                String(config.value("display.timezone", "Asia/Shanghai")),
              ),
            })}
          </p>
          <label className="form-field">
            <span>{t("admin:security.memberships.code")}</span>
            <Input
              readOnly
              className="font-mono"
              value={result.code}
              autoComplete="off"
              onFocus={(event) => event.currentTarget.select()}
            />
          </label>
          <div className="form-actions">
            <Button
              type="button"
              onClick={() => {
                setResult(undefined);
                onClose();
              }}
            >
              <Check />
              {t("admin:security.savedCodes")}
            </Button>
          </div>
        </div>
      ) : (
        <form
          className="entity-form"
          onSubmit={(event) => {
            void form.handleSubmit(async ({ username, realm }) => {
              if (busy.current) return;
              busy.current = true;
              setError(undefined);
              const input = {
                username,
                ...(realm && { realm }),
              };
              const payload = JSON.stringify(input);
              if (request.current.payload !== payload)
                request.current = { payload, key: crypto.randomUUID() };
              try {
                const response = await identityMembershipInvite(
                  input,
                  request.current.key,
                );
                if (active.current) {
                  form.reset();
                  setResult(response.data);
                }
              } catch (cause) {
                if (active.current) setError(cause);
              } finally {
                busy.current = false;
              }
            })(event);
          }}
        >
          {!!error && <ErrorBanner error={error} />}
          <label className="form-field">
            <span id="invite-login-label">
              {t("admin:security.memberships.login")}
            </span>
            <Input
              required
              autoComplete="off"
              maxLength={80}
              aria-labelledby="invite-login-label"
              aria-describedby="invite-login-help"
              aria-invalid={!!form.formState.errors.username}
              {...form.register("username")}
            />
            <small id="invite-login-help">
              {t(
                form.formState.errors.username
                  ? "admin:security.memberships.invalidLogin"
                  : "admin:security.memberships.loginHelp",
              )}
            </small>
          </label>
          <label className="form-field">
            <span>{t("admin:login.realm")}</span>
            <Input
              autoComplete="off"
              maxLength={63}
              aria-invalid={!!form.formState.errors.realm}
              {...form.register("realm")}
            />
            <small>
              {t(
                form.formState.errors.realm
                  ? "admin:login.realmInvalid"
                  : "admin:login.realmHelp",
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
                <Send />
              )}
              {t("admin:security.memberships.generate")}
            </Button>
          </div>
        </form>
      )}
    </EditorDialog>
  );
}
