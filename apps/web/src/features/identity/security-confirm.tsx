import { useEffect, useRef, useState, type ReactNode } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useTranslation } from "react-i18next";
import { Check, LoaderCircle, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { EditorDialog, ErrorBanner } from "./shared";

const schema = z.object({
  password: z.string().min(1).max(128),
  factor: z
    .string()
    .trim()
    .regex(/^(?:[0-9]{6}|[a-f0-9]{32})?$/),
});

export function SecurityConfirm({
  title,
  description,
  onClose,
  onConfirm,
  requireFactor = false,
  showFactor = requireFactor,
  children,
}: {
  title: string;
  description: string;
  onClose: () => void;
  requireFactor?: boolean;
  showFactor?: boolean;
  children?: ReactNode;
  onConfirm: (
    password: string,
    key: string,
    signal: AbortSignal,
    factor: string,
  ) => Promise<void>;
}) {
  const { t } = useTranslation();
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
    defaultValues: { password: "", factor: "" },
  });
  return (
    <EditorDialog
      open
      onOpenChange={(open) => {
        if (!open && !submitting.current) onClose();
      }}
      title={title}
      description={description}
    >
      <form
        className="entity-form"
        onSubmit={(event) => {
          void form.handleSubmit(async ({ password, factor }) => {
            if (submitting.current) return;
            submitting.current = true;
            setError(null);
            const signal = operation.current!.signal;
            try {
              await onConfirm(password, key.current, signal, factor);
              if (!signal.aborted) onClose();
            } catch (cause) {
              if (signal.aborted) return;
              setError(cause);
              key.current = crypto.randomUUID();
              form.resetField("password");
              form.resetField("factor");
            } finally {
              submitting.current = false;
            }
          })(event);
        }}
      >
        {!!error && <ErrorBanner error={error} />}
        {children}
        <label className="form-field">
          <span>{t("admin:login.password")}</span>
          <Input
            type="password"
            autoComplete="current-password"
            required
            maxLength={128}
            {...form.register("password")}
          />
        </label>
        {showFactor && (
          <label className="form-field">
            <span>{t("admin:login.secondFactorCode")}</span>
            <Input
              required={requireFactor}
              type="password"
              maxLength={32}
              autoComplete="one-time-code"
              aria-invalid={!!form.formState.errors.factor}
              {...form.register("factor")}
            />
            {form.formState.errors.factor && (
              <small>{t("admin:login.secondFactorInvalid")}</small>
            )}
          </label>
        )}
        <div className="form-actions">
          <Button
            type="button"
            variant="outline"
            disabled={form.formState.isSubmitting}
            onClick={onClose}
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
            {t("admin:security.confirm")}
          </Button>
        </div>
      </form>
    </EditorDialog>
  );
}
