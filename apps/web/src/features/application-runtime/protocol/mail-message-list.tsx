import { ChevronLeft, ChevronRight, Inbox, Send } from "lucide-react";
import { useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ShortDate } from "@/features/identity/shared";
import type { MailMessageListItem } from "@/lib/capabilities";
import { MailAnalysis } from "./mail-analysis";
import type { ComponentRendererProps } from "./component-registry";

type MessagePage = {
  items: MailMessageListItem[];
  total: number;
  page: number;
  pageSize: number;
};

export function MailMessageList({
  data,
  dataRefreshing,
  props,
  readOnly,
  events,
  emit,
}: ComponentRendererProps) {
  const { t } = useTranslation("mail");
  const [, setParams] = useSearchParams();
  const folder = props.folder === "sent" ? "sent" : "inbox";
  const page = data as MessagePage | null;
  const items = page?.items ?? [];
  const total = page?.total ?? 0;
  const current = page?.page ?? 1;
  const size = page?.pageSize ?? 50;
  const canGoBack = current > 1;
  const canGoForward = current * size < total;
  function changePage(nextPage: number) {
    setParams((old) => {
      const next = new URLSearchParams(old);
      if (nextPage > 1) next.set("page", String(nextPage));
      else next.delete("page");
      return next;
    });
  }

  const EmptyIcon = folder === "sent" ? Send : Inbox;
  return (
    <section
      className="mail-list"
      aria-label={t(folder === "sent" ? "sent" : "inbox")}
      aria-busy={dataRefreshing || undefined}
    >
      {!readOnly && folder === "inbox" && events?.includes("recommend") && (
        <div className="border-b border-border px-3 py-3">
          <MailAnalysis event="recommend" emit={emit} />
        </div>
      )}
      {(total > 0 || canGoBack) && (
        <nav
          className="mail-list-pagination flex items-center justify-end gap-3 px-3 py-1"
          aria-label={t("message.pagination")}
        >
          <span className="text-xs text-muted-foreground">
            {items.length > 0 &&
              t("message.range", {
                start: (current - 1) * size + 1,
                end: (current - 1) * size + items.length,
                total,
              })}
          </span>
          <div className="flex gap-1">
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={t("message.previousPage")}
              disabled={!canGoBack || dataRefreshing}
              onClick={() => changePage(current - 1)}
            >
              <ChevronLeft />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={t("message.nextPage")}
              disabled={!canGoForward || dataRefreshing}
              onClick={() => changePage(current + 1)}
            >
              <ChevronRight />
            </Button>
          </div>
        </nav>
      )}
      {items.length > 0 ? (
        <ul className="divide-y divide-border">
          {items.map((message) => {
            const unread = folder === "inbox" && !message.read;
            const subject = message.subject || t("message.noSubject");
            return (
              <li key={message.id}>
                <Button
                  variant="ghost"
                  className="mail-list-row"
                  type="button"
                  data-unread={unread || undefined}
                  disabled={readOnly || dataRefreshing}
                  aria-label={subject}
                  aria-describedby={`mail-row-${message.id}`}
                  onClick={() => void emit("open", { id: message.id })}
                >
                  {folder === "inbox" && (
                    <span className="mail-row-marker" aria-hidden="true" />
                  )}
                  <span className="mail-row-sender">
                    {message.from || t("message.unknownSender")}
                  </span>
                  <span className="mail-row-copy">
                    <span className="mail-row-subject">{subject}</span>
                    {message.snippet && (
                      <span className="mail-row-snippet">
                        {message.snippet}
                      </span>
                    )}
                  </span>
                  <span className="mail-row-meta" id={`mail-row-${message.id}`}>
                    <ShortDate value={message.occurredAt} />
                    {folder === "inbox" &&
                      (unread ? (
                        <Badge
                          variant="secondary"
                          className="mail-unread-badge"
                        >
                          {t("message.readState.false")}
                        </Badge>
                      ) : (
                        <span>{t("message.readState.true")}</span>
                      ))}
                  </span>
                </Button>
              </li>
            );
          })}
        </ul>
      ) : (
        <div className="grid justify-items-center gap-3 px-6 py-12 text-sm text-muted-foreground">
          <EmptyIcon aria-hidden className="size-6" />
          <p>{t(folder === "sent" ? "emptySent" : "emptyInbox")}</p>
        </div>
      )}
    </section>
  );
}
