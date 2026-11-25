import { useEffect, useId, useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router";
import { Controller, useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { LoaderCircle, Paperclip, Send, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  NativeSelect,
  NativeSelectOption,
} from "@/components/ui/native-select";
import { FormField } from "@/components/shared/form-field";
import { ApiError } from "@/api/client/request";
import { EditorDialog, ErrorBanner } from "@/features/identity/shared";
import {
  filesUpload,
  type MailAddress,
  type Mailbox,
} from "@/lib/capabilities";
import type { ComponentRendererProps } from "./component-registry";
import { MailComposeAssistant } from "./mail-compose-assistant";
import { MailHtmlEditor } from "./mail-html-editor";

const composeSchema = z.object({
  mailboxId: z.number().int().positive(),
  to: z.string().trim().min(3, "required"),
  cc: z.string(),
  subject: z.string().trim().min(1, "required"),
  html: z.string().min(1, "required").max(100000, "bodyLimit"),
});
type ComposeForm = z.infer<typeof composeSchema>;
type ComposeAttachment = { fileId: number; filename: string };

function parseAddresses(value: string): MailAddress[] {
  return value
    .split(/[,;]/)
    .map((item) => item.trim())
    .filter(Boolean)
    .map((address) => ({ address }));
}

function quoteMarkup(value: string) {
  if (!value.trim()) return "";
  const text = value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replace(/\r?\n/g, "<br>");
  return `<blockquote>${text}</blockquote>`;
}

function readBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      if (typeof reader.result === "string")
        resolve(reader.result.split(",")[1] ?? "");
      else reject(new Error("file_unavailable"));
    };
    reader.onerror = () => reject(new Error("file_unavailable"));
    reader.readAsDataURL(file);
  });
}

export function ComposePanel({
  data,
  readOnly,
  events,
  emit,
}: ComponentRendererProps) {
  const { t } = useTranslation("mail");
  const location = useLocation();
  const navigate = useNavigate();
  const bodyLabel = useId();
  const [draftKey, setDraftKey] = useState<{ payload: string; key: string }>();
  const preset = (location.state ?? {}) as {
    to?: string;
    cc?: string;
    subject?: string;
    quote?: string;
    returnTo?: "/mail" | "/mail/sent";
    attachments?: ComposeAttachment[];
  };
  const activeMailboxes = useMemo(() => {
    return ((data as { items: Mailbox[] } | null)?.items ?? []).filter(
      (item) => item.status === "active",
    );
  }, [data]);
  const defaultMailbox =
    activeMailboxes.find((item) => item.defaultSend) ?? activeMailboxes[0];
  const form = useForm<ComposeForm>({
    resolver: zodResolver(composeSchema),
    defaultValues: {
      mailboxId: defaultMailbox?.id ?? 0,
      to: preset.to ?? "",
      cc: preset.cc ?? "",
      subject: preset.subject ?? "",
      html: quoteMarkup(preset.quote ?? ""),
    },
  });
  const [to, cc, subject, html] = useWatch({
    control: form.control,
    name: ["to", "cc", "subject", "html"],
  });
  const [insertion, setInsertion] = useState<{ text: string }>();
  const [error, setError] = useState<unknown>();
  const [attachments, setAttachments] = useState<ComposeAttachment[]>(
    preset.attachments ?? [],
  );
  const [uploading, setUploading] = useState(false);
  const [confirmDiscard, setConfirmDiscard] = useState(false);
  useEffect(() => {
    const current = form.getValues("mailboxId");
    if (!activeMailboxes.some((mailbox) => mailbox.id === current))
      form.setValue("mailboxId", defaultMailbox?.id ?? 0);
  }, [activeMailboxes, defaultMailbox, form]);
  const { errors, isSubmitting, isDirty } = form.formState;
  const leave = () =>
    navigate(preset.returnTo === "/mail/sent" ? "/mail/sent" : "/mail");
  const submit = form.handleSubmit(async (values) => {
    if (readOnly || uploading) return;
    setError(undefined);
    const payload = {
      mailboxId: values.mailboxId,
      to: parseAddresses(values.to),
      cc: parseAddresses(values.cc),
      subject: values.subject,
      html: values.html,
      fileIds: attachments.map((attachment) => attachment.fileId),
    };
    const serialized = JSON.stringify(payload);
    const clientKey =
      draftKey?.payload === serialized ? draftKey.key : crypto.randomUUID();
    if (draftKey?.key !== clientKey)
      setDraftKey({ payload: serialized, key: clientKey });
    const sent = await emit(
      "submit",
      { ...payload, clientKey },
      {
        ephemeral: true,
        onError: (code) => setError(new ApiError(code, 400)),
      },
    );
    if (sent) await emit("sent");
  });
  return (
    <form
      className="entity-form grid w-full max-w-5xl gap-4 rounded-xl border border-border bg-card p-4 sm:p-6"
      onSubmit={submit}
      noValidate
    >
      {!readOnly && events?.includes("suggest-draft") && (
        <MailComposeAssistant
          emit={emit}
          disabled={isSubmitting}
          values={{ to, cc, subject }}
          html={html}
          getValue={(field) => form.getValues(field)}
          getHTML={() => form.getValues("html")}
          setValue={(field, value) =>
            form.setValue(field, value, {
              shouldDirty: true,
              shouldValidate: true,
            })
          }
          insertBody={(text) => setInsertion({ text })}
        />
      )}
      <fieldset disabled={readOnly || isSubmitting}>
        <div className="mail-compose-fields">
          <FormField label={t("compose.from")} wide>
            <NativeSelect
              className="w-full"
              {...form.register("mailboxId", { valueAsNumber: true })}
            >
              {!defaultMailbox && (
                <NativeSelectOption value={0}>
                  {t("compose.noMailbox")}
                </NativeSelectOption>
              )}
              {activeMailboxes.map((mailbox) => (
                <NativeSelectOption key={mailbox.id} value={mailbox.id}>
                  {mailbox.emailAddress}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </FormField>
          <FormField label={t("compose.to")} error={errors.to?.message}>
            <Input {...form.register("to")} />
          </FormField>
          <FormField label={t("compose.cc")} error={errors.cc?.message}>
            <Input {...form.register("cc")} />
          </FormField>
          <FormField
            label={t("compose.subject")}
            error={errors.subject?.message}
            wide
          >
            <Input {...form.register("subject")} />
          </FormField>
          <div className="editor-field field-wide">
            <span id={bodyLabel}>{t("compose.body")}</span>
            <Controller
              name="html"
              control={form.control}
              render={({ field }) => (
                <MailHtmlEditor
                  quote={preset.quote ?? ""}
                  html={field.value}
                  insertion={insertion}
                  readOnly={readOnly || isSubmitting}
                  labelledBy={bodyLabel}
                  errorId={errors.html ? `${bodyLabel}-error` : undefined}
                  onChange={field.onChange}
                  onBlur={field.onBlur}
                />
              )}
            />
            {errors.html && (
              <p role="alert" className="field-error" id={`${bodyLabel}-error`}>
                {t(
                  errors.html.message === "bodyLimit"
                    ? "compose.bodyLimit"
                    : "compose.bodyRequired",
                )}
              </p>
            )}
          </div>
        </div>
      </fieldset>
      {attachments.length > 0 && (
        <ul className="grid gap-2">
          {attachments.map((attachment) => (
            <li
              key={attachment.fileId}
              className="flex min-w-0 items-center justify-between gap-2 text-sm"
            >
              <span className="break-all">{attachment.filename}</span>
              <Button
                type="button"
                variant="outline"
                disabled={readOnly || isSubmitting}
                onClick={() =>
                  setAttachments((current) =>
                    current.filter((item) => item.fileId !== attachment.fileId),
                  )
                }
              >
                <X />
                {t("compose.removeAttachment")}
              </Button>
            </li>
          ))}
        </ul>
      )}
      {error !== undefined && <ErrorBanner error={error} />}
      <div className="mail-compose-actions">
        <div className="flex flex-wrap items-center gap-2">
          <Button
            type="submit"
            className="mail-compose-send"
            disabled={readOnly || isSubmitting || uploading || !defaultMailbox}
          >
            {isSubmitting ? (
              <LoaderCircle className="animate-spin motion-reduce:animate-none" />
            ) : (
              <Send />
            )}
            {t("compose.send")}
          </Button>
          <label className="mail-attachment-picker">
            {uploading ? (
              <LoaderCircle className="size-4 animate-spin" />
            ) : (
              <Paperclip className="size-4" />
            )}
            <span>{t("compose.attachments")}</span>
            <input
              className="sr-only"
              type="file"
              multiple
              disabled={
                readOnly ||
                isSubmitting ||
                uploading ||
                attachments.length >= 20
              }
              onChange={async (event) => {
                const files = Array.from(event.target.files ?? []).slice(
                  0,
                  20 - attachments.length,
                );
                event.target.value = "";
                if (!files.length) return;
                setUploading(true);
                setError(undefined);
                try {
                  const uploaded = await Promise.all(
                    files.map(async (file) => ({
                      file,
                      response: await filesUpload({
                        name: file.name,
                        purpose: "mail_attachment",
                        content: await readBase64(file),
                      }),
                    })),
                  );
                  setAttachments((current) => [
                    ...current,
                    ...uploaded.map((item) => ({
                      fileId: item.response.data.id,
                      filename: item.response.data.name,
                    })),
                  ]);
                } catch (error) {
                  setError(error);
                } finally {
                  setUploading(false);
                }
              }}
            />
          </label>
        </div>
        <Button
          type="button"
          variant="outline"
          disabled={isSubmitting || uploading}
          onClick={() => {
            if (
              isDirty ||
              attachments.length !== (preset.attachments?.length ?? 0)
            )
              setConfirmDiscard(true);
            else void leave();
          }}
        >
          <X />
          {t("compose.cancel")}
        </Button>
      </div>
      <EditorDialog
        open={confirmDiscard}
        onOpenChange={setConfirmDiscard}
        title={t("compose.discardTitle")}
        description={t("compose.discardHelp")}
      >
        <div className="flex justify-end gap-2">
          <Button
            type="button"
            variant="outline"
            onClick={() => setConfirmDiscard(false)}
          >
            <X />
            {t("compose.keepEditing")}
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={() => void leave()}
          >
            <X />
            {t("compose.discard")}
          </Button>
        </div>
      </EditorDialog>
    </form>
  );
}
