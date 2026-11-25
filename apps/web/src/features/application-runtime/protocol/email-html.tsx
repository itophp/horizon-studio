import { useEffect, useRef, useState } from "react";
import { CodeXml, FileText, Image } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";

const readerHead = `<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<style>
  :root { color-scheme: light dark; }
  body { margin: 0; padding: clamp(16px, 3vw, 28px); font: 15px/1.65 system-ui, sans-serif; overflow-wrap: anywhere; }
  p, ul, ol, pre, blockquote { margin-block: 0 0.85em; }
  blockquote { margin-inline: 0; padding-inline-start: 16px; border-inline-start: 3px solid GrayText; }
  pre { white-space: pre-wrap; } :where(table, img) { max-width: 100%; }
</style>`;

export function HtmlMailFrame({
  html,
  title,
  showImages = false,
}: {
  html: string;
  title: string;
  showImages?: boolean;
}) {
  const observer = useRef<ResizeObserver | null>(null);
  const [height, setHeight] = useState<number>();
  const imageSource = showImages ? "https:" : "'none'";
  useEffect(() => () => observer.current?.disconnect(), []);
  return (
    <iframe
      className="mail-html-frame"
      style={height ? { height } : undefined}
      title={title}
      sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
      referrerPolicy="no-referrer"
      srcDoc={`<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src ${imageSource}; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'">${readerHead}</head><body>${html}</body></html>`}
      onLoad={({ currentTarget }) => {
        observer.current?.disconnect();
        // The sandbox omits script execution. Same-origin access lets the
        // parent measure the body without running code inside the email.
        const doc = currentTarget.contentDocument;
        if (!doc?.body || doc.URL !== "about:srcdoc") return;
        const body = doc.body;
        const resize = () =>
          setHeight(
            Math.max(96, Math.ceil(body.getBoundingClientRect().height)),
          );
        resize();
        if (typeof ResizeObserver !== "undefined") {
          observer.current = new ResizeObserver(resize);
          observer.current.observe(body);
        }
      }}
    />
  );
}

export function EmailHtml({ html, text }: { html?: string; text: string }) {
  const { t } = useTranslation("mail");
  const [plain, setPlain] = useState(false);
  const [showImages, setShowImages] = useState(false);
  const canSwitch = !!html && !!text.trim();
  const hasImages = !!html && /<img\b[^>]*\ssrc="https:\/\//i.test(html);
  const plainBody = text.trim() ? (
    <pre className="mail-plain-body">{text}</pre>
  ) : (
    <p className="text-sm text-muted-foreground">{t("message.noBody")}</p>
  );
  return (
    <section className="mail-reader-body" aria-label={t("message.body")}>
      <div className="mail-reader-format">
        <span className="inline-flex items-center gap-2 text-xs font-medium">
          {html && !plain ? (
            <CodeXml className="size-4" />
          ) : (
            <FileText className="size-4" />
          )}
          {t(html && !plain ? "message.htmlFormat" : "message.textFormat")}
        </span>
        <div className="flex flex-wrap items-center gap-1">
          {!plain && hasImages && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              aria-pressed={showImages}
              onClick={() => setShowImages((value) => !value)}
            >
              <Image />
              {t(showImages ? "message.hideImages" : "message.showImages")}
            </Button>
          )}
          {canSwitch && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setPlain((value) => !value)}
            >
              {plain ? <CodeXml /> : <FileText />}
              {t(plain ? "message.showHtml" : "message.showPlainText")}
            </Button>
          )}
        </div>
      </div>
      {html && !plain ? (
        <HtmlMailFrame
          html={html}
          title={t("message.body")}
          showImages={showImages}
        />
      ) : (
        plainBody
      )}
    </section>
  );
}
