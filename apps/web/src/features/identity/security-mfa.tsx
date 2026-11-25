import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import {
  Check,
  KeyRound,
  LoaderCircle,
  RefreshCw,
  ShieldOff,
  X,
} from "lucide-react";
import type { IdentityMfaEnrollOutput } from "@/api/generated/capability-identity";
import {
  identityMfaStatus,
  identityMfaEnroll,
  identityMfaActivate,
  identityMfaActivateReplacement,
  identityMfaReplace,
  identityMfaDisable,
  identityMfaReauthenticate,
  identityMfaRenewRecoveryCodes,
} from "@/lib/capabilities";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ErrorBanner, Loading } from "./shared";
import { SecurityConfirm } from "./security-confirm";

const schema = z.object({ code: z.string().regex(/^[0-9]{6}$/) });

export function SecurityMFA({ queryKey }: { queryKey: readonly unknown[] }) {
  const { t } = useTranslation();
  const status = useQuery({
    queryKey: [...queryKey, "mfa"],
    queryFn: identityMfaStatus,
    retry: false,
  });
  const [enrolling, setEnrolling] = useState(false);
  const [manage, setManage] = useState<
    "disable" | "renewRecoveryCodes" | "replace"
  >();
  const [enrollment, setEnrollment] = useState<
    IdentityMfaEnrollOutput & { replacement?: boolean }
  >();
  const [codes, setCodes] = useState<string[]>();
  const [error, setError] = useState<unknown>(null);
  const key = useRef(crypto.randomUUID());
  const submitting = useRef(false);
  const operation = useRef<AbortController | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    operation.current = controller;
    return () => controller.abort();
  }, []);
  const form = useForm({
    resolver: zodResolver(schema),
    defaultValues: { code: "" },
  });
  if (status.isPending) return <Loading />;
  if (status.error)
    return (
      <ErrorBanner error={status.error} onRetry={() => void status.refetch()} />
    );
  return (
    <section className="admin-panel grid gap-4 p-5">
      <h2>{t("admin:security.authenticator")}</h2>
      {!!error && <ErrorBanner error={error} />}
      {codes ? (
        <>
          <p role="status">{t("admin:security.recoveryOnce")}</p>
          <ul className="grid gap-2 sm:grid-cols-2">
            {codes.map((code) => (
              <li key={code}>
                <code className="break-all select-all">{code}</code>
              </li>
            ))}
          </ul>
          <Button type="button" onClick={() => setCodes(undefined)}>
            <Check />
            {t("admin:security.savedCodes")}
          </Button>
        </>
      ) : enrollment ? (
        <form
          className="entity-form"
          noValidate
          onSubmit={(event) => {
            void form.handleSubmit(async ({ code }) => {
              if (submitting.current) return;
              submitting.current = true;
              setError(null);
              const signal = operation.current!.signal;
              try {
                const activate = enrollment.replacement
                  ? identityMfaActivateReplacement
                  : identityMfaActivate;
                const response = await activate({ code }, key.current, signal);
                if (signal.aborted) return;
                setEnrollment(undefined);
                form.reset();
                setCodes(response.data.recoveryCodes);
                await status.refetch();
              } catch (cause) {
                if (signal.aborted) return;
                setError(cause);
                key.current = crypto.randomUUID();
              } finally {
                submitting.current = false;
              }
            })(event);
          }}
        >
          <p>{t("admin:security.enrollmentHelp")}</p>
          <code className="break-all select-all">{enrollment.secret}</code>
          <label className="form-field">
            <span>{t("admin:security.authenticatorCode")}</span>
            <Input
              autoComplete="one-time-code"
              inputMode="numeric"
              maxLength={6}
              aria-invalid={!!form.formState.errors.code}
              {...form.register("code")}
            />
          </label>
          {form.formState.errors.code && (
            <p role="alert">{t("admin:login.secondFactorInvalid")}</p>
          )}
          <div className="form-actions">
            <Button
              type="button"
              variant="outline"
              disabled={form.formState.isSubmitting}
              onClick={() => setEnrollment(undefined)}
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
              {t("admin:security.activate")}
            </Button>
          </div>
        </form>
      ) : (
        <>
          <p role="status">
            {t(
              status.data?.data.enabled
                ? "admin:security.mfaEnabled"
                : "admin:security.mfaDisabled",
              { count: status.data?.data.recoveryRemaining ?? 0 },
            )}
          </p>
          {!status.data?.data.enabled && (
            <Button type="button" onClick={() => setEnrolling(true)}>
              <KeyRound />
              {t("admin:security.enroll")}
            </Button>
          )}
          {status.data?.data.enabled && (
            <div className="form-actions">
              <Button
                type="button"
                variant="outline"
                onClick={() => setManage("replace")}
              >
                <KeyRound />
                {t("admin:security.replaceAuthenticator")}
              </Button>
              <Button
                type="button"
                variant="outline"
                onClick={() => setManage("renewRecoveryCodes")}
              >
                <RefreshCw />
                {t("admin:security.renewRecovery")}
              </Button>
              <Button
                type="button"
                variant="outline"
                onClick={() => setManage("disable")}
              >
                <ShieldOff />
                {t("admin:security.disableAuthenticator")}
              </Button>
            </div>
          )}
        </>
      )}
      {enrolling && (
        <SecurityConfirm
          title={t("admin:security.enroll")}
          description={t("admin:security.enrollmentHelp")}
          onClose={() => setEnrolling(false)}
          onConfirm={async (password, operationKey, signal) => {
            const response = await identityMfaEnroll(
              { password },
              operationKey,
              signal,
            );
            if (signal.aborted) return;
            key.current = crypto.randomUUID();
            setEnrollment(response.data);
          }}
        />
      )}
      {manage && (
        <SecurityConfirm
          requireFactor
          title={t(
            manage === "disable"
              ? "admin:security.disableAuthenticator"
              : manage === "replace"
                ? "admin:security.replaceAuthenticator"
                : "admin:security.renewRecovery",
          )}
          description={t("admin:security.confirmFactors")}
          onClose={() => setManage(undefined)}
          onConfirm={async (password, operationKey, signal, factor) => {
            const proof = await identityMfaReauthenticate(
              {
                password,
                operation: manage,
                ...(factor.length === 6
                  ? { code: factor }
                  : { recoveryCode: factor }),
              },
              signal,
            );
            if (signal.aborted) return;
            if (manage === "replace") {
              const result = await identityMfaReplace(
                { proof: proof.data.proof },
                operationKey,
                signal,
              );
              if (!signal.aborted) {
                key.current = crypto.randomUUID();
                setEnrollment({ ...result.data, replacement: true });
              }
              return;
            }
            const execute =
              manage === "disable"
                ? identityMfaDisable
                : identityMfaRenewRecoveryCodes;
            const response = await execute(
              { proof: proof.data.proof },
              operationKey,
              signal,
            );
            if (signal.aborted) return;
            if (response.data.recoveryCodes.length)
              setCodes(response.data.recoveryCodes);
            await status.refetch();
          }}
        />
      )}
    </section>
  );
}
