import { useEffect, useRef, useState } from "react";
import { Check, LoaderCircle, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { EditorDialog, ErrorBanner } from "./shared";

export function IAMDecisionDialog({
  title,
  description,
  requireReason = true,
  onConfirm,
  onClose,
  onSaved,
}: {
  title: string;
  description: string;
  requireReason?: boolean;
  onConfirm: (
    reason: string,
    key: string,
    signal: AbortSignal,
  ) => Promise<unknown>;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>();
  const controller = useRef<AbortController | null>(null);
  const attempt = useRef<{ reason: string; key: string } | undefined>(
    undefined,
  );
  useEffect(() => {
    const active = new AbortController();
    controller.current = active;
    return () => active.abort();
  }, []);
  return (
    <EditorDialog
      open
      title={title}
      description={description}
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      <form
        className="entity-form"
        onSubmit={async (event) => {
          event.preventDefault();
          const signal = controller.current?.signal;
          if (!signal || signal.aborted || pending) return;
          const reason = String(
            new FormData(event.currentTarget).get("reason") ?? "",
          ).trim();
          if (requireReason && reason.length < 5) return;
          if (attempt.current?.reason !== reason)
            attempt.current = { reason, key: crypto.randomUUID() };
          const { key } = attempt.current;
          setError(undefined);
          setPending(true);
          try {
            await onConfirm(reason, key, signal);
            if (!signal.aborted) onSaved();
          } catch (cause) {
            if (!signal.aborted) setError(cause);
          } finally {
            if (!signal.aborted) setPending(false);
          }
        }}
      >
        {!!error && <ErrorBanner error={error} />}
        {requireReason && (
          <label className="form-field">
            <span>{t("admin:policy.reason")}</span>
            <Textarea
              name="reason"
              required
              minLength={5}
              maxLength={500}
              disabled={pending}
            />
          </label>
        )}
        <div className="form-actions">
          <Button
            type="button"
            variant="outline"
            disabled={pending}
            onClick={onClose}
          >
            <X />
            {t("admin:actions.cancel")}
          </Button>
          <Button type="submit" disabled={pending}>
            {pending ? <LoaderCircle className="animate-spin" /> : <Check />}
            {t("admin:security.confirm")}
          </Button>
        </div>
      </form>
    </EditorDialog>
  );
}
