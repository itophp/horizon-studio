import { useEffect, useRef, useState } from "react";
import { Check, LoaderCircle, Mail, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  applicationCompleteRegistration,
  applicationRequestRegistration,
} from "./application-authentication";
import { ApiError } from "@/api/client/request";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { EditorDialog, ErrorBanner } from "./shared";

export function RegistrationDialog({
  application,
  onClose,
}: {
  application?: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [stage, setStage] = useState<"request" | "verify" | "done">("request");
  const [error, setError] = useState<unknown>();
  const [pending, setPending] = useState(false);
  const password = useRef("");
  const request = useRef<AbortController | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    request.current = controller;
    return () => {
      controller.abort();
      password.current = "";
    };
  }, []);
  return (
    <EditorDialog
      open
      title={t("admin:registration.title")}
      description={t(
        stage === "request"
          ? "admin:registration.help"
          : "admin:registration.delivery",
      )}
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      {stage === "done" ? (
        <>
          <p role="status">{t("admin:registration.done")}</p>
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
            if (pending || !signal) return;
            const form = new FormData(event.currentTarget);
            const value = (key: string) => String(form.get(key) ?? "");
            setPending(true);
            setError(undefined);
            const run = async () => {
              if (stage === "request") {
                const entered = value("password");
                const result = await applicationRequestRegistration(
                  application,
                  {
                    username: value("username").trim(),
                    displayName: value("displayName").trim(),
                    destination: value("destination").trim(),
                    password: entered,
                  },
                  { signal },
                );
                if (result.status !== 204)
                  throw new ApiError("invalid", result.status);
                if (!signal.aborted) {
                  password.current = entered;
                  setStage("verify");
                }
              } else {
                const result = await applicationCompleteRegistration(
                  application,
                  { token: value("code").trim(), password: password.current },
                  { signal },
                );
                if (result.status !== 204)
                  throw new ApiError("forbidden", result.status);
                if (!signal.aborted) {
                  password.current = "";
                  setStage("done");
                }
              }
            };
            void run()
              .catch((cause: unknown) => {
                if (!signal.aborted) setError(cause);
              })
              .finally(() => {
                if (!signal.aborted) setPending(false);
              });
          }}
        >
          {!!error && <ErrorBanner error={error} />}
          <fieldset className="grid gap-3" disabled={pending}>
            {stage === "request" ? (
              <>
                <label className="form-field">
                  <span>{t("admin:login.username")}</span>
                  <Input
                    name="username"
                    required
                    minLength={3}
                    maxLength={80}
                    autoComplete="username"
                  />
                </label>
                <label className="form-field">
                  <span>{t("admin:registration.displayName")}</span>
                  <Input
                    name="displayName"
                    required
                    maxLength={100}
                    autoComplete="name"
                  />
                </label>
                <label className="form-field">
                  <span>{t("admin:registration.destination")}</span>
                  <Input
                    name="destination"
                    required
                    minLength={3}
                    maxLength={254}
                  />
                </label>
                <label className="form-field">
                  <span>{t("admin:login.password")}</span>
                  <Input
                    name="password"
                    type="password"
                    required
                    minLength={12}
                    maxLength={128}
                    autoComplete="new-password"
                  />
                </label>
              </>
            ) : (
              <label className="form-field">
                <span>{t("admin:recovery.code")}</span>
                <Input
                  name="code"
                  type="password"
                  required
                  pattern="[a-f0-9]{64}"
                  maxLength={64}
                  autoComplete="one-time-code"
                />
              </label>
            )}
          </fieldset>
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
            <Button type="submit" disabled={pending}>
              {pending ? (
                <LoaderCircle className="animate-spin" />
              ) : stage === "request" ? (
                <Mail />
              ) : (
                <Check />
              )}
              {t(
                stage === "request"
                  ? "admin:recovery.sendCode"
                  : "admin:recovery.verifyCode",
              )}
            </Button>
          </div>
        </form>
      )}
    </EditorDialog>
  );
}
