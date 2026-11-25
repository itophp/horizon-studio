import { useEffect, useRef, useState } from "react";
import { Link, useLocation, useNavigate } from "react-router";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useTranslation } from "react-i18next";
import { Check, ArrowLeft } from "lucide-react";
import { Button, buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { externalIdentityLink } from "@/lib/capabilities";
import { useAuth } from "./auth";
import { BrandMark, ErrorBanner, Loading } from "./shared";
import { FederationProviders } from "./federation-providers";

const schema = z.object({
  secondFactor: z
    .string()
    .trim()
    .regex(/^(?:[0-9]{6}|[0-9a-f]{32})?$/),
  password: z.string().max(128),
});

// Wait for the initial session query before capturing the one-time callback.
// AuthProvider remounts its subtree when the authenticated account changes.
export function FederationCallbackPage({
  protocol = "oidc",
}: {
  protocol?: "oidc" | "saml";
}) {
  const auth = useAuth();
  return auth.loading ? (
    <Loading />
  ) : (
    <FederationCallback protocol={protocol} />
  );
}

function FederationCallback({ protocol }: { protocol: "oidc" | "saml" }) {
  const { t } = useTranslation();
  const auth = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [callback] = useState(() => {
    const query = new URLSearchParams(location.search);
    return {
      code: query.get("code"),
      state: query.get("state"),
      failed: query.has("error"),
    };
  });
  const [proof, setProof] = useState<string>();
  const [error, setError] = useState<unknown>(null);
  const [outcome, setOutcome] = useState<"linked" | "reviewRequired">();
  const [reviewOpened, setReviewOpened] = useState(false);
  const [consumed, setConsumed] = useState(false);
  const submitted = useRef(false);
  const key = useRef(crypto.randomUUID());
  const form = useForm({
    resolver: zodResolver(schema),
    defaultValues: { secondFactor: "", password: "" },
  });
  const valid =
    protocol === "saml" ||
    (!!callback.code &&
      callback.code.length <= 4096 &&
      /^[a-f0-9]{64}$/.test(callback.state ?? "") &&
      !callback.failed);
  useEffect(() => {
    // Router replacement removes the authorization code from browser history.
    // The original callback remains only in this component's memory.
    if (location.search) navigate(location.pathname, { replace: true });
  }, [location.pathname, location.search, navigate]);
  return (
    <main className="login-screen">
      <section className="login-form-side w-full">
        <div className="login-form-wrap">
          <BrandMark />
          <h1>{t("admin:federation.title")}</h1>
          <p className="login-subtitle">{t("admin:federation.description")}</p>
          {!!error && <ErrorBanner error={error} />}
          {outcome ? (
            <>
              <p role="status">
                {t(
                  outcome === "linked"
                    ? "admin:federation.linked"
                    : "admin:federation.reviewRequired",
                )}
              </p>
              {reviewOpened && (
                <Link
                  className={buttonVariants({ variant: "outline" })}
                  to="/account-security?tab=external"
                >
                  {t("admin:externalReview.open")}
                </Link>
              )}
            </>
          ) : proof ? (
            auth.profile ? (
              <form
                onSubmit={(event) => {
                  void form.handleSubmit(async (values) => {
                    if (submitted.current || !values.password) return;
                    submitted.current = true;
                    setError(null);
                    try {
                      const response = await externalIdentityLink(
                        { proof, password: values.password },
                        key.current,
                      );
                      setProof(undefined);
                      form.reset();
                      setReviewOpened(!!response.data.reviewId);
                      setOutcome(
                        response.data.linked ? "linked" : "reviewRequired",
                      );
                    } catch (cause) {
                      setError(cause);
                      form.resetField("password");
                    } finally {
                      submitted.current = false;
                    }
                  })(event);
                }}
              >
                <p className="login-help">
                  {t("admin:federation.linkAccount", {
                    account: auth.profile.account.username,
                  })}
                </p>
                <label className="login-field">
                  <span>{t("admin:login.password")}</span>
                  <Input
                    type="password"
                    autoComplete="current-password"
                    required
                    maxLength={128}
                    {...form.register("password")}
                  />
                </label>
                <Button type="submit" disabled={form.formState.isSubmitting}>
                  <Check />
                  {t("admin:federation.confirmLink")}
                </Button>
              </form>
            ) : (
              <p role="alert">{t("admin:federation.sessionRequired")}</p>
            )
          ) : valid && !consumed ? (
            <form
              noValidate
              onSubmit={(event) => {
                void form.handleSubmit(async (values) => {
                  if (submitted.current) return;
                  submitted.current = true;
                  setConsumed(true);
                  setError(null);
                  try {
                    const factor = values.secondFactor;
                    const secondFactor = {
                      ...(factor.length === 6 && { mfaCode: factor }),
                      ...(factor.length === 32 && { recoveryCode: factor }),
                    };
                    const linkProof =
                      protocol === "saml"
                        ? await auth.finishSaml(secondFactor)
                        : await auth.finishFederation({
                            code: callback.code!,
                            state: callback.state!,
                            ...secondFactor,
                          });
                    form.reset();
                    if (linkProof) setProof(linkProof);
                    else navigate("/", { replace: true });
                  } catch (cause) {
                    setError(cause);
                  } finally {
                    submitted.current = false;
                  }
                })(event);
              }}
            >
              <label className="login-field">
                <span id="federation-factor-label">
                  {t("admin:login.secondFactorCode")}
                </span>
                <Input
                  aria-labelledby="federation-factor-label"
                  aria-describedby="federation-factor-help"
                  autoComplete="one-time-code"
                  autoCapitalize="none"
                  spellCheck={false}
                  maxLength={32}
                  aria-invalid={!!form.formState.errors.secondFactor}
                  {...form.register("secondFactor")}
                />
                <small id="federation-factor-help">
                  {t("admin:federation.optionalFactor")}
                </small>
              </label>
              {form.formState.errors.secondFactor && (
                <p role="alert">{t("admin:login.secondFactorInvalid")}</p>
              )}
              <Button type="submit" disabled={form.formState.isSubmitting}>
                <Check />
                {t("admin:federation.complete")}
              </Button>
            </form>
          ) : consumed ? (
            <p role="status">
              {t(
                form.formState.isSubmitting
                  ? "admin:login.signingIn"
                  : "admin:federation.restart",
              )}
            </p>
          ) : (
            <>
              {callback.failed && (
                <p role="alert">{t("admin:federation.restart")}</p>
              )}
              {auth.profile ? (
                <FederationProviders purpose="link" />
              ) : (
                <p>{t("admin:federation.sessionRequired")}</p>
              )}
            </>
          )}
          <Link
            className={buttonVariants({ variant: "link" })}
            to={auth.profile ? "/" : "/login"}
          >
            <ArrowLeft />
            {t("admin:federation.back")}
          </Link>
        </div>
      </section>
    </main>
  );
}
