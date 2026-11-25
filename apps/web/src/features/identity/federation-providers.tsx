import { useState } from "react";
import { Link, LogIn } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  identityStartFederation,
  useIdentityFederationProviders,
} from "@/api/generated/baseline";
import { ApiError } from "@/api/client/request";
import { Button } from "@/components/ui/button";
import { ErrorBanner, Loading } from "./shared";

export function FederationProviders({
  purpose,
}: {
  purpose: "login" | "link";
}) {
  const { t } = useTranslation();
  const providers = useIdentityFederationProviders({ query: { retry: false } });
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  if (providers.isPending) return <Loading />;
  if (providers.error) return <ErrorBanner error={providers.error} />;
  const items = providers.data?.status === 200 ? providers.data.data : [];
  return (
    <div className="grid gap-2">
      {!!error && <ErrorBanner error={error} />}
      {items.map((provider) => (
        <Button
          key={provider.id}
          type="button"
          variant="outline"
          disabled={pending}
          onClick={async () => {
            setPending(true);
            setError(null);
            try {
              const response = await identityStartFederation({
                providerId: provider.id,
                purpose,
              });
              if (response.status !== 200)
                throw new ApiError("unavailable", response.status);
              window.location.assign(response.data.authorizationUrl);
            } catch (cause) {
              setError(cause);
              setPending(false);
            }
          }}
        >
          {purpose === "login" ? <LogIn /> : <Link />}
          {t(
            purpose === "login"
              ? "admin:federation.signInWith"
              : "admin:federation.linkWith",
            { provider: provider.name },
          )}
        </Button>
      ))}
      {items.length === 0 && purpose === "link" && (
        <p className="login-help">{t("admin:federation.noProviders")}</p>
      )}
    </div>
  );
}
