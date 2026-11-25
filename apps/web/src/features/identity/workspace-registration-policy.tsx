import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, LoaderCircle, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
  identityConfigureRegistration,
  identityRegistrationPolicy,
} from "@/lib/capabilities";
import { EditorDialog, ErrorBanner, Loading } from "./shared";

type Channel = "invite" | "email" | "sms";

export function WorkspaceRegistrationPolicy({
  queryKey,
  onClose,
}: {
  queryKey: readonly unknown[];
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const policy = useQuery({
    queryKey: [...queryKey, "registration-policy"],
    queryFn: ({ signal }) => identityRegistrationPolicy(signal),
    retry: false,
  });
  const [selected, setSelected] = useState<Channel>();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>();
  const request = useRef<AbortController | null>(null);
  const attempt = useRef<{ selected: Channel; key: string } | undefined>(
    undefined,
  );
  useEffect(() => {
    const controller = new AbortController();
    request.current = controller;
    return () => controller.abort();
  }, []);
  const channel = selected ?? policy.data?.data.channel;
  return (
    <EditorDialog
      open
      title={t("admin:registration.settings")}
      description={t("admin:registration.settingsHelp")}
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      {!!(policy.error || error) && (
        <ErrorBanner error={policy.error || error} />
      )}
      {policy.isPending && <Loading />}
      {channel && (
        <form
          className="entity-form"
          onSubmit={(event) => {
            event.preventDefault();
            const signal = request.current?.signal;
            if (!signal || pending) return;
            if (attempt.current?.selected !== channel)
              attempt.current = { selected: channel, key: crypto.randomUUID() };
            setPending(true);
            setError(undefined);
            void identityConfigureRegistration(
              { channel },
              attempt.current.key,
              signal,
            )
              .then(() => {
                if (!signal.aborted) {
                  void policy.refetch();
                  onClose();
                }
              })
              .catch((cause: unknown) => {
                if (!signal.aborted) setError(cause);
              })
              .finally(() => {
                if (!signal.aborted) setPending(false);
              });
          }}
        >
          <label className="form-field">
            <span>{t("admin:registration.mode")}</span>
            <select
              value={channel}
              onChange={(event) => {
                const value = event.target.value;
                if (value === "email" || value === "sms" || value === "invite")
                  setSelected(value);
              }}
              disabled={pending}
            >
              <option value="invite">
                {t("admin:registration.inviteOnly")}
              </option>
              <option value="email">{t("admin:recovery.verifyEmail")}</option>
              <option value="sms">{t("admin:recovery.verifyPhone")}</option>
            </select>
          </label>
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
              {pending ? <LoaderCircle className="animate-spin" /> : <Check />}
              {t("admin:actions.save")}
            </Button>
          </div>
        </form>
      )}
    </EditorDialog>
  );
}
