import { useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, Check, LoaderCircle, LogOut } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  identityRedeemSignInInvitation,
  identityListSignInIdentities,
} from "@/api/generated/baseline";
import { ApiError } from "@/api/client/request";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useAuth } from "./auth";
import { ErrorBanner, Loading } from "./shared";

export function LoginIdentityOptions() {
  const { t } = useTranslation();
  const auth = useAuth();
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [accepted, setAccepted] = useState(false);
  const [error, setError] = useState<unknown>();
  const identities = useQuery({
    queryKey: ["sign-in-identities"],
    retry: false,
    queryFn: async ({ signal }) => {
      const response = await identityListSignInIdentities({ signal });
      if (response.status !== 200)
        throw new ApiError("unauthenticated", response.status);
      return response.data;
    },
  });
  async function run(action: () => Promise<unknown>) {
    if (busy) return;
    setBusy(true);
    setError(undefined);
    try {
      await action();
    } catch (cause) {
      setError(cause);
    } finally {
      setBusy(false);
    }
  }
  function join(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const candidate = code.trim();
    if (!/^[a-f0-9]{64}$/.test(candidate)) return;
    void run(async () => {
      const response = await identityRedeemSignInInvitation({
        code: candidate,
      });
      if (response.status !== 204)
        throw new ApiError("forbidden", response.status);
      setCode("");
      setAccepted(true);
      await identities.refetch();
    });
  }
  return (
    <main className="login-screen">
      <section className="login-form-wrap space-y-4">
        <h1>{t("admin:login.accessOptions")}</h1>
        {!!(error || identities.error) && (
          <ErrorBanner error={error || identities.error} />
        )}
        {identities.isPending && <Loading />}
        {accepted && <p role="status">{t("admin:login.invitationAccepted")}</p>}
        {!identities.isPending &&
          identities.data?.length === 0 &&
          !accepted && <p>{t("admin:login.accessPending")}</p>}
        {identities.data?.map((identity) => (
          <Button
            key={identity.identityBindingId ?? "base"}
            type="button"
            variant="outline"
            disabled={busy}
            className="w-full justify-between"
            onClick={() =>
              void run(() =>
                auth.selectSignInIdentity(identity.identityBindingId),
              )
            }
          >
            {identity.actingOrganizationName ??
              identity.identityType ??
              t("admin:security.memberships.base")}
            <ArrowRight />
          </Button>
        ))}
        <form className="entity-form" onSubmit={join}>
          <label className="form-field">
            <span>{t("admin:security.memberships.code")}</span>
            <Input
              value={code}
              type="password"
              maxLength={64}
              autoComplete="off"
              disabled={busy}
              onChange={(event) => setCode(event.target.value)}
            />
          </label>
          <Button
            type="submit"
            disabled={busy || !/^[a-f0-9]{64}$/.test(code.trim())}
          >
            {busy ? <LoaderCircle className="animate-spin" /> : <Check />}
            {t("admin:security.memberships.accept")}
          </Button>
        </form>
        <Button
          type="button"
          variant="ghost"
          disabled={busy}
          onClick={() => void run(auth.signOut)}
        >
          <LogOut />
          {t("admin:actions.signOut")}
        </Button>
      </section>
    </main>
  );
}
