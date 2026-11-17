import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Empty, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { useSession } from "@/features/administration/auth";
import { ErrorBanner, Loading } from "@/features/administration/shared";
import { useApplicationCatalog } from "@/features/application-runtime/catalog";

export function StudioApplicationAccess({ children }: { children: ReactNode }) {
  const { t } = useTranslation("studio");
  const profile = useSession();
  const catalog = useApplicationCatalog();

  const empty = (title: string) => (
    <Empty role="alert">
      <EmptyHeader>
        <EmptyTitle>{title}</EmptyTitle>
      </EmptyHeader>
    </Empty>
  );
  if (!profile.permissions.includes("studio:read"))
    return empty(t("forbidden"));
  if (catalog.isPending) return <Loading />;
  if (catalog.isError)
    return (
      <ErrorBanner
        error={catalog.error}
        onRetry={() => void catalog.refetch()}
      />
    );
  if (!catalog.data.items.some((item) => item.id === "studio"))
    return empty(t("applicationUnavailable"));

  return children;
}
