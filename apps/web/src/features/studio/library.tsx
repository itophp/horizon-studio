import type { ReactNode } from "react";
import { ChevronRight, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { ErrorBanner, Loading } from "@/features/administration/shared";

export type Selection = (id: string) => void;
type RecordRow = { id: string; title: string; subtitle?: string };
type BrowserProps = {
  entries: RecordRow[];
  selected?: string;
  onSelect: Selection;
  heading: string;
  loading?: boolean;
  fetching?: boolean;
  error?: unknown;
  refresh: () => void;
  action?: ReactNode;
  list?: ReactNode;
  children: ReactNode;
};

export function Library({
  entries,
  selected,
  onSelect,
  heading,
  loading,
  fetching,
  error,
  refresh,
  action,
  list,
  children,
}: BrowserProps) {
  const { t } = useTranslation("studio");
  return (
    <>
      <aside className="studio-tasks flex min-h-0 min-w-0 flex-col">
        <header className="shrink-0 px-3 pt-3 pb-1">
          <div className="flex items-center justify-between gap-2">
            <h2 className="text-sm font-semibold">{heading}</h2>
            <Button
              type="button"
              size="icon-sm"
              variant="ghost"
              disabled={fetching}
              aria-label={t("refresh")}
              title={t("refresh")}
              onClick={refresh}
            >
              <RefreshCw
                className={fetching ? "motion-safe:animate-spin" : undefined}
              />
            </Button>
          </div>
          {action}
        </header>
        <div className="studio-task-scroll min-h-0 max-h-64 overflow-y-auto px-2 pb-2">
          {loading && <Loading />}
          {error ? <ErrorBanner error={error} onRetry={refresh} /> : null}
          {!list && !loading && !error && !entries.length && (
            <p className="px-3 py-6 text-center text-sm text-muted-foreground">
              {t("catalogEmpty")}
            </p>
          )}
          {list ?? (
            <ul className="grid content-start gap-1">
              {entries.map((row) => (
                <li key={row.id} className="min-w-0">
                  <Button
                    type="button"
                    variant="ghost"
                    aria-pressed={row.id === selected}
                    className="h-auto min-h-12 w-full justify-start gap-2 rounded-lg px-2 py-1.5 text-left whitespace-normal aria-pressed:bg-sidebar-accent"
                    onClick={() => onSelect(row.id)}
                  >
                    <span className="min-w-0 flex-1">
                      <span className="line-clamp-2 block text-sm leading-5 [overflow-wrap:anywhere]">
                        {row.title}
                      </span>
                      {row.subtitle && (
                        <>
                          {" "}
                          <span className="block truncate text-xs font-normal text-muted-foreground">
                            {row.subtitle}
                          </span>
                        </>
                      )}
                    </span>
                    {row.id === selected && (
                      <ChevronRight className="size-3.5" aria-hidden="true" />
                    )}
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </aside>
      <section className="studio-record min-h-80 min-w-0 overflow-auto">
        {children}
      </section>
    </>
  );
}

export function RecordHeader({
  title,
  subtitle,
  badge,
  action,
}: {
  title: string;
  subtitle?: string;
  badge?: string;
  action?: ReactNode;
}) {
  return (
    <header className="flex flex-wrap items-start justify-between gap-4 px-5 py-5 sm:px-7">
      <div className="min-w-0">
        {subtitle && (
          <p className="mb-1 text-xs font-medium text-muted-foreground [overflow-wrap:anywhere]">
            {subtitle}
          </p>
        )}
        <h2 className="text-xl leading-tight font-semibold tracking-tight [overflow-wrap:anywhere]">
          {title}
        </h2>
        {badge && (
          <Badge variant="secondary" className="mt-2">
            {badge}
          </Badge>
        )}
      </div>
      {action}
    </header>
  );
}

export function LibraryEmpty({ icon }: { icon: ReactNode }) {
  const { t } = useTranslation("studio");
  return (
    <Empty className="min-h-80">
      <EmptyHeader>
        <EmptyMedia variant="icon">{icon}</EmptyMedia>
        <EmptyTitle>{t("selectEntry")}</EmptyTitle>
      </EmptyHeader>
    </Empty>
  );
}

export function DetailField({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <section className="space-y-2">
      <h3 className="text-sm font-medium">{label}</h3>
      <p className="text-sm leading-relaxed whitespace-pre-wrap text-muted-foreground [overflow-wrap:anywhere]">
        {children}
      </p>
    </section>
  );
}
