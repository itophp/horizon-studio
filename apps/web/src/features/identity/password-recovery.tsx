import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, LoaderCircle, Mail, X } from "lucide-react";
import {
  applicationCompletePasswordReset,
  applicationRequestPasswordReset,
} from "./application-authentication";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { EditorDialog, ErrorBanner } from "./shared";
import { ApiError } from "@/api/client/request";

export function PasswordRecovery({
  application,
  onClose,
}: {
  application?: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [stage, setStage] = useState<"request" | "reset" | "done">("request");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const request = useRef<AbortController | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    request.current = controller;
    return () => controller.abort();
  }, []);
  return (
    <EditorDialog
      open
      title={t("admin:recovery.title")}
      description={t(
        stage === "request" ? "admin:recovery.help" : "admin:recovery.delivery",
      )}
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
    >
      {stage === "done" ? (
        <>
          <p role="status">{t("admin:recovery.done")}</p>
          <Button type="button" onClick={onClose}>
            <Check />
            {t("admin:actions.close")}
          </Button>
        </>
      ) : (
        <form
          key={stage}
          className="entity-form"
          onSubmit={(event) => {
            event.preventDefault();
            const signal = request.current?.signal;
            if (busy || !signal) return;
            const data = new FormData(event.currentTarget);
            const get = (key: string) => String(data.get(key) ?? "");
            setBusy(true);
            setError(undefined);
            const process = async () => {
              if (stage === "request") {
                const response = await applicationRequestPasswordReset(
                  application,
                  {
                    username: get("username"),
                    channel: get("channel") === "sms" ? "sms" : "email",
                  },
                  { signal },
                );
                if (response.status !== 204)
                  throw new ApiError("unavailable", response.status);
                if (!signal.aborted) setStage("reset");
                return;
              }
              const factor = get("factor").trim();
              const response = await applicationCompletePasswordReset(
                application,
                {
                  token: get("token").trim(),
                  newPassword: get("newPassword"),
                  ...(factor.length === 6
                    ? { mfaCode: factor }
                    : factor
                      ? { recoveryCode: factor }
                      : {}),
                },
                { signal },
              );
              if (response.status !== 204)
                throw new ApiError("forbidden", response.status);
              if (!signal.aborted) setStage("done");
            };
            void process()
              .catch((cause: unknown) => {
                if (!signal.aborted) setError(cause);
              })
              .finally(() => {
                if (!signal.aborted) setBusy(false);
              });
          }}
        >
          {!!error && <ErrorBanner error={error} />}
          <fieldset className="grid gap-3" disabled={busy}>
            {stage === "request" ? (
              <>
                <label className="form-field">
                  <span>{t("admin:login.username")}</span>
                  <Input
                    name="username"
                    autoComplete="username"
                    required
                    minLength={3}
                    maxLength={80}
                  />
                </label>
                <label className="form-field">
                  <span>{t("admin:recovery.channel")}</span>
                  <select name="channel">
                    <option value="email">{t("admin:fields.email")}</option>
                    <option value="sms">{t("admin:recovery.phone")}</option>
                  </select>
                </label>
              </>
            ) : (
              <>
                <label className="form-field">
                  <span>{t("admin:recovery.code")}</span>
                  <Input
                    name="token"
                    type="password"
                    required
                    pattern="[0-9a-f]{64}"
                    maxLength={64}
                    autoComplete="one-time-code"
                  />
                </label>
                <label className="form-field">
                  <span>{t("admin:recovery.newPassword")}</span>
                  <Input
                    name="newPassword"
                    type="password"
                    required
                    minLength={12}
                    maxLength={128}
                    autoComplete="new-password"
                  />
                </label>
                <label className="form-field">
                  <span>{t("admin:login.secondFactorCode")}</span>
                  <Input
                    name="factor"
                    type="password"
                    pattern="([0-9]{6}|[a-f0-9]{32})"
                    maxLength={32}
                    autoComplete="off"
                  />
                  <small>{t("admin:federation.optionalFactor")}</small>
                </label>
              </>
            )}
          </fieldset>
          <div className="form-actions">
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={onClose}
            >
              <X />
              {t("admin:actions.cancel")}
            </Button>
            <Button type="submit" disabled={busy}>
              {busy ? (
                <LoaderCircle className="animate-spin" />
              ) : stage === "request" ? (
                <Mail />
              ) : (
                <Check />
              )}
              {t(
                stage === "request"
                  ? "admin:recovery.sendCode"
                  : "admin:recovery.reset",
              )}
            </Button>
          </div>
        </form>
      )}
    </EditorDialog>
  );
}
