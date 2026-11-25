import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, LoaderCircle, MailPlus, Smartphone } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  identityContactList,
  identityContactStart,
  identityContactVerify,
  identityMfaReauthenticate,
  identityMfaStatus,
} from "@/lib/capabilities";
import { EditorDialog, ErrorBanner, Loading, ShortDate } from "./shared";
import { SecurityConfirm } from "./security-confirm";

export function SecurityContacts({
  queryKey,
}: {
  queryKey: readonly unknown[];
}) {
  const { t } = useTranslation();
  const [channel, setChannel] = useState<"email" | "sms">("email");
  const [destination, setDestination] = useState("");
  const [stage, setStage] = useState<"contact" | "confirm" | "verify">();
  const [verifying, setVerifying] = useState(false);
  const [error, setError] = useState<unknown>();
  const request = useRef<AbortController | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    request.current = controller;
    return () => controller.abort();
  }, []);
  const contacts = useQuery({
    queryKey: [...queryKey, "contacts"],
    queryFn: ({ signal }) => identityContactList(signal),
    retry: false,
  });
  const mfa = useQuery({
    queryKey: [...queryKey, "mfa"],
    queryFn: identityMfaStatus,
    retry: false,
  });
  const title = t("admin:recovery.contacts");
  const channelLabel = (value: "email" | "sms") =>
    t(value === "email" ? "admin:fields.email" : "admin:recovery.phone");
  return (
    <section className="admin-panel grid gap-4 p-5">
      <h2>{title}</h2>
      {contacts.isPending || mfa.isPending ? (
        <Loading />
      ) : (
        <>
          {!!(contacts.error || mfa.error || error) && (
            <ErrorBanner error={contacts.error || mfa.error || error} />
          )}
          <dl className="grid gap-2">
            {contacts.data?.data.items.map((item) => (
              <div key={item.channel} className="flex flex-wrap gap-3">
                <dt>{channelLabel(item.channel)}</dt>
                <dd>{item.destination}</dd>
                <dd>
                  <ShortDate value={item.verifiedAt} />
                </dd>
              </div>
            ))}
          </dl>
          <div className="form-actions">
            <Button
              type="button"
              variant="outline"
              disabled={!!mfa.error}
              onClick={() => {
                setChannel("email");
                setDestination("");
                setStage("contact");
              }}
            >
              <MailPlus />
              {t("admin:recovery.verifyEmail")}
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={!!mfa.error}
              onClick={() => {
                setChannel("sms");
                setDestination("");
                setStage("contact");
              }}
            >
              <Smartphone />
              {t("admin:recovery.verifyPhone")}
            </Button>
          </div>
        </>
      )}
      {stage === "contact" && (
        <EditorDialog
          open
          title={channelLabel(channel)}
          description={t("admin:recovery.help")}
          onOpenChange={(open) => {
            if (!open) setStage(undefined);
          }}
        >
          <form
            className="entity-form"
            onSubmit={(event) => {
              event.preventDefault();
              setStage("confirm");
            }}
          >
            <label className="form-field">
              <span>{channelLabel(channel)}</span>
              <Input
                type={channel === "email" ? "email" : "tel"}
                required
                pattern={channel === "sms" ? "\\+[1-9][0-9]{7,14}" : undefined}
                maxLength={channel === "email" ? 254 : 16}
                value={destination}
                onChange={(event) => setDestination(event.target.value)}
              />
            </label>
            <Button type="submit">
              <Check />
              {t("admin:actions.next")}
            </Button>
          </form>
        </EditorDialog>
      )}
      {stage === "confirm" && (
        <SecurityConfirm
          title={t("admin:recovery.sendCode")}
          description={destination}
          requireFactor={!!mfa.data?.data.enabled}
          onClose={() =>
            setStage((current) => (current === "confirm" ? undefined : current))
          }
          onConfirm={async (password, key, signal, factor) => {
            const result = await identityMfaReauthenticate(
              {
                password,
                operation: "verifyContact",
                ...(factor.length === 6
                  ? { code: factor }
                  : factor
                    ? { recoveryCode: factor }
                    : {}),
              },
              signal,
            );
            if (signal.aborted) return;
            await identityContactStart(
              { channel, destination, proof: result.data.proof },
              key,
              signal,
            );
            if (!signal.aborted) {
              setError(undefined);
              setStage("verify");
            }
          }}
        />
      )}
      {stage === "verify" && (
        <EditorDialog
          open
          title={t("admin:recovery.verifyCode")}
          description={t("admin:recovery.codeSent")}
          onOpenChange={(open) => {
            if (!open && !verifying) setStage(undefined);
          }}
        >
          {!!error && <ErrorBanner error={error} />}
          <form
            className="entity-form"
            onSubmit={(event) => {
              event.preventDefault();
              const signal = request.current?.signal;
              if (verifying || !signal) return;
              const code = String(
                new FormData(event.currentTarget).get("code") ?? "",
              ).trim();
              setError(undefined);
              setVerifying(true);
              void identityContactVerify({ code }, crypto.randomUUID(), signal)
                .then(() => {
                  if (!signal.aborted) {
                    setDestination("");
                    setStage(undefined);
                    void contacts.refetch();
                  }
                })
                .catch((cause: unknown) => {
                  if (!signal.aborted) setError(cause);
                })
                .finally(() => {
                  if (!signal.aborted) setVerifying(false);
                });
            }}
          >
            <label className="form-field">
              <span>{t("admin:recovery.code")}</span>
              <Input
                name="code"
                required
                type="password"
                pattern="[a-f0-9]{64}"
                autoComplete="one-time-code"
                maxLength={64}
                disabled={verifying}
              />
            </label>
            <Button type="submit" disabled={verifying}>
              {verifying ? (
                <LoaderCircle className="animate-spin" />
              ) : (
                <Check />
              )}
              {t("admin:recovery.verifyCode")}
            </Button>
          </form>
        </EditorDialog>
      )}
    </section>
  );
}
