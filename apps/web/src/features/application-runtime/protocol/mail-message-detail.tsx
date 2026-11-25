import { useContext, useEffect, useId, useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import {
  ArrowDownToLine,
  ArrowLeft,
  Forward,
  Languages,
  Reply,
  ReplyAll,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button, buttonVariants } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import type { MailMessageTranslateOutput } from "@/api/generated/capability-mail";
import { ApiError } from "@/api/client/request";
import { ErrorBanner, ShortDate } from "@/features/identity/shared";
import {
  mailAttachmentRead,
  type Mailbox,
  type MailMessage,
} from "@/lib/capabilities";
import { MailAnalysis } from "./mail-analysis";
import { EmailHtml } from "./email-html";
import {
  PresentationPermissions,
  type ComponentRendererProps,
} from "./component-registry";

export function MessageDetailPanel({ data, ...props }: ComponentRendererProps) {
  const { i18n } = useTranslation("mail");
  const record = data as MailMessage | null;
  const locale = i18n.resolvedLanguage?.startsWith("zh") ? "zh-CN" : "en";
  if (!record) return null;
  return (
    <MessageDetail
      key={`${record.id}-${locale}-${props.readOnly}`}
      record={record}
      locale={locale}
      {...props}
    />
  );
}

function MessageDetail({
  record,
  locale,
  dataMap,
  readOnly,
  events,
  emit,
}: ComponentRendererProps & { record: MailMessage; locale: "zh-CN" | "en" }) {
  const { t } = useTranslation("mail");
  const navigate = useNavigate();
  const permissions = useContext(PresentationPermissions);
  const canSend =
    !readOnly &&
    permissions.includes("mail:send") &&
    permissions.includes("mail:configure");
  const [error, setError] = useState<unknown>();
  const [translation, setTranslation] = useState<MailMessageTranslateOutput>();
  const [showTranslation, setShowTranslation] = useState(false);
  const [translating, setTranslating] = useState(false);
  const [translationFailed, setTranslationFailed] = useState(false);
  const translationHint = useId();
  const displayedTranslation = showTranslation ? translation : undefined;
  const canTranslate =
    !readOnly &&
    events?.includes("translate") &&
    Boolean(
      record.subject?.trim() ||
      record.bodyText?.trim() ||
      record.bodyHtml?.trim(),
    );

  async function toggleTranslation() {
    if (translation) {
      setShowTranslation(!showTranslation);
      return;
    }
    setTranslating(true);
    setTranslationFailed(false);
    try {
      let response: MailMessageTranslateOutput | undefined;
      const succeeded = await emit(
        "translate",
        { locale },
        {
          ephemeral: true,
          onResult: (data) => {
            response = data as MailMessageTranslateOutput;
          },
          onError: () => setTranslationFailed(true),
        },
      );
      if (!succeeded || !response) {
        setTranslationFailed(true);
        return;
      }
      setTranslation(response);
      setShowTranslation(true);
    } catch {
      setTranslationFailed(true);
    } finally {
      setTranslating(false);
    }
  }

  const marked = useRef<number>(0);
  useEffect(() => {
    if (!record.read && !readOnly && marked.current !== record.id) {
      marked.current = record.id;
      void emit(
        "read",
        { id: record.id },
        {
          ephemeral: true,
          onError: (code) => setError(new ApiError(code, 400)),
        },
      );
    }
  }, [record, readOnly, emit]);
  const recipients = record.recipients ?? [];
  const attachments = record.attachments ?? [];
  const bodyText = record.bodyText ?? "";
  const mailboxes = dataMap?._mailboxes as { items?: Mailbox[] } | undefined;
  const ownAddress =
    mailboxes?.items?.find((item) => item.id === record.mailboxId)
      ?.emailAddress ?? "";
  const originalRecipients = recipients
    .filter((item) => item.kind === "to" || item.kind === "cc")
    .map((item) => item.address);
  const otherRecipients = originalRecipients.filter(
    (address) => address !== ownAddress,
  );
  const returnTo = record.folder === "sent" ? "/mail/sent" : "/mail";
  const replyState = {
    to: record.from,
    subject: record.subject.startsWith("Re:")
      ? record.subject
      : `Re: ${record.subject}`,
    quote: bodyText,
    returnTo,
  };
  const allState = {
    to:
      record.from !== ownAddress && record.from
        ? record.from
        : (otherRecipients.shift() ?? ""),
    cc: otherRecipients.join(", "),
    subject: record.subject.startsWith("Re:")
      ? record.subject
      : `Re: ${record.subject}`,
    quote: bodyText,
    returnTo,
  };
  const forwardState = {
    subject: record.subject.startsWith("Fwd:")
      ? record.subject
      : `Fwd: ${record.subject}`,
    quote: bodyText,
    returnTo,
    attachments: attachments.map((attachment) => ({
      fileId: attachment.fileId,
      filename: attachment.filename,
    })),
  };
  return (
    <article className="mx-auto grid w-full max-w-6xl min-w-0 overflow-hidden rounded-xl border border-border bg-card">
      <div className="border-b border-border px-3 py-2">
        <Link className={buttonVariants({ variant: "ghost" })} to={returnTo}>
          <ArrowLeft />
          {t(record.folder === "sent" ? "backToSent" : "backToMail")}
        </Link>
      </div>
      <div className="grid min-w-0 gap-4 p-4 sm:px-7 sm:py-5">
        <header className="grid gap-3">
          <h1 className="text-xl font-semibold wrap-anywhere sm:text-2xl">
            {(displayedTranslation?.subject ?? record.subject) ||
              t("message.noSubject")}
          </h1>
          <dl className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-4 gap-y-1 text-sm">
            <dt className="text-muted-foreground">{t("compose.from")}</dt>
            <dd className="wrap-anywhere">
              {record.from || t("message.unknownSender")}
            </dd>
            {(["to", "cc"] as const).map((kind) => {
              const addresses = recipients.filter(
                (recipient) => recipient.kind === kind,
              );
              if (addresses.length === 0) return null;
              return (
                <RecipientLine
                  key={kind}
                  label={t(kind === "to" ? "compose.to" : "compose.cc")}
                  addresses={addresses}
                />
              );
            })}
            <dt className="text-muted-foreground">{t("message.date")}</dt>
            <dd>
              <ShortDate value={record.occurredAt} />
            </dd>
          </dl>
        </header>
        {error !== undefined && <ErrorBanner error={error} />}
        {canTranslate && (
          <div className="grid gap-2">
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={translating}
                aria-describedby={translationHint}
                onClick={() => void toggleTranslation()}
              >
                {translating ? (
                  <Spinner aria-hidden />
                ) : (
                  <Languages aria-hidden />
                )}
                {t(
                  showTranslation
                    ? "translation.original"
                    : translation
                      ? "translation.translated"
                      : "translation.action",
                )}
              </Button>
              <p id={translationHint} className="text-xs text-muted-foreground">
                {t("translation.hint")}
              </p>
            </div>
            {translating && (
              <p role="status" className="text-sm">
                {t("translation.pending")}
              </p>
            )}
            {translationFailed && (
              <p role="alert" className="text-sm text-destructive">
                {t("translation.failed")}
              </p>
            )}
            {displayedTranslation?.truncated && (
              <p className="text-sm text-muted-foreground">
                {t("translation.truncated")}
              </p>
            )}
          </div>
        )}
        {!readOnly && events?.includes("analyze") && (
          <MailAnalysis
            key={`analysis-${record.id}`}
            event="analyze"
            emit={emit}
          />
        )}
        <EmailHtml
          html={
            displayedTranslation
              ? displayedTranslation.bodyHtml
              : record.bodyHtml
          }
          text={displayedTranslation?.body ?? bodyText}
        />
        {!readOnly && attachments.length > 0 && (
          <ul className="flex flex-wrap gap-2">
            {attachments.map((attachment) => (
              <li key={attachment.id} className="min-w-0">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => {
                    setError(undefined);
                    void downloadAttachment(
                      record.id,
                      attachment.id,
                      attachment.filename,
                    ).catch(setError);
                  }}
                >
                  <ArrowDownToLine />
                  <span className="max-w-56 truncate">
                    {attachment.filename}
                  </span>
                </Button>
              </li>
            ))}
          </ul>
        )}
        {canSend && (
          <div className="flex flex-wrap gap-2 border-t border-border pt-4">
            <Button
              type="button"
              variant="outline"
              onClick={() => navigate("/mail/compose", { state: replyState })}
            >
              <Reply />
              {t("message.reply")}
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={() => navigate("/mail/compose", { state: allState })}
            >
              <ReplyAll />
              {t("message.replyAll")}
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={() => navigate("/mail/compose", { state: forwardState })}
            >
              <Forward />
              {t("message.forward")}
            </Button>
          </div>
        )}
      </div>
    </article>
  );
}

function RecipientLine({
  label,
  addresses,
}: {
  label: string;
  addresses: MailMessage["recipients"];
}) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="wrap-anywhere">
        {addresses
          .map(({ address, displayName }) =>
            displayName ? `${displayName} <${address}>` : address,
          )
          .join(", ")}
      </dd>
    </>
  );
}

async function downloadAttachment(
  messageId: number,
  attachmentId: number,
  filename: string,
) {
  const result = await mailAttachmentRead(messageId, attachmentId);
  const binary = atob(result.data.content);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1)
    bytes[index] = binary.charCodeAt(index);
  const url = URL.createObjectURL(new Blob([bytes]));
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  URL.revokeObjectURL(url);
}
