import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Check, LoaderCircle, MailPlus, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import { FormField } from "@/components/shared/form-field";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ApiError } from "@/api/client/request";
import { EditorDialog, ErrorBanner } from "@/features/identity/shared";
import type { Mailbox } from "@/lib/capabilities";
import type { ComponentRendererProps } from "./component-registry";
import {
  MailConnectionFields,
  mailboxSchema,
  serverSettings,
} from "./mail-connection-fields";

const passwordRule = z.string().min(1, "required");
const credentialSchema = z.object({ password: passwordRule });

function MailDialogActions({
  busy,
  label,
  connect,
  onClose,
}: {
  busy: boolean;
  label: string;
  connect?: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation("admin");
  const Icon = connect ? MailPlus : Check;
  return (
    <div className="form-actions">
      <Button type="button" variant="outline" disabled={busy} onClick={onClose}>
        <X />
        {t("actions.cancel")}
      </Button>
      <Button type="submit" disabled={busy}>
        {busy ? (
          <LoaderCircle className="animate-spin motion-reduce:animate-none" />
        ) : (
          <Icon />
        )}
        {label}
      </Button>
    </div>
  );
}

function MailboxForm({
  emit,
  mailbox,
  onClose,
  onSaved,
}: {
  emit: ComponentRendererProps["emit"];
  mailbox?: Mailbox;
  onClose: () => void;
  onSaved: (verified: boolean) => void;
}) {
  const { t } = useTranslation("mail");
  const [error, setError] = useState<unknown>();
  const form = useForm<z.infer<typeof mailboxSchema>>({
    resolver: zodResolver(
      mailbox
        ? mailboxSchema
        : mailboxSchema.extend({ password: passwordRule }),
    ),
    defaultValues: {
      name: mailbox?.name ?? "",
      emailAddress: mailbox?.emailAddress ?? "",
      ...serverSettings(mailbox),
      password: "",
    },
  });
  const { errors, isSubmitting } = form.formState;
  const submit = form.handleSubmit(async ({ password, ...values }) => {
    setError(undefined);
    const payload = mailbox
      ? {
          ...values,
          id: mailbox.id,
          revision: mailbox.revision,
          defaultSend: mailbox.defaultSend,
        }
      : { ...values, password };
    let verified = Boolean(mailbox);
    if (
      await emit(mailbox ? "update" : "connect", payload, {
        ephemeral: true,
        onResult: (result) => {
          if (!mailbox)
            verified =
              (result as { status?: string } | undefined)?.status === "active";
        },
        onError: (code) => setError(new ApiError(code, 400)),
      })
    )
      onSaved(verified);
  });
  return (
    <EditorDialog
      open
      title={t(mailbox ? "mailbox.edit" : "mailbox.connect")}
      description={t(mailbox ? "mailbox.editHelp" : "mailbox.autoHint")}
      onOpenChange={(open) => {
        if (!open && !isSubmitting) onClose();
      }}
    >
      <form className="entity-form" onSubmit={submit} noValidate>
        <fieldset className="grid gap-5" disabled={isSubmitting}>
          <div className="editor-grid">
            <FormField label={t("mailbox.name")} error={errors.name?.message}>
              <Input maxLength={120} {...form.register("name")} />
            </FormField>
            <FormField
              label={t("mailbox.address")}
              error={errors.emailAddress?.message}
            >
              <Input
                type="email"
                autoComplete="email"
                maxLength={254}
                {...form.register("emailAddress")}
              />
            </FormField>
            {!mailbox && (
              <FormField
                label={t("mailbox.password")}
                error={errors.password?.message}
                wide
              >
                <Input
                  type="password"
                  autoComplete="new-password"
                  maxLength={4096}
                  {...form.register("password")}
                />
              </FormField>
            )}
          </div>
          <MailConnectionFields form={form} connecting={!mailbox} />
        </fieldset>
        {error !== undefined && <ErrorBanner error={error} />}
        <MailDialogActions
          busy={isSubmitting}
          label={t(mailbox ? "mailbox.save" : "mailbox.connect")}
          connect={!mailbox}
          onClose={onClose}
        />
      </form>
    </EditorDialog>
  );
}

function MailCredentialForm({
  emit,
  mailbox,
  purpose,
  onClose,
  onSaved,
}: {
  emit: ComponentRendererProps["emit"];
  mailbox: Mailbox;
  purpose: "smtp" | "pop3" | "imap";
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation("mail");
  const [error, setError] = useState<unknown>();
  const form = useForm<z.infer<typeof credentialSchema>>({
    resolver: zodResolver(credentialSchema),
    defaultValues: { password: "" },
  });
  const { errors, isSubmitting } = form.formState;
  const label = t(
    purpose === "smtp"
      ? "mailbox.newSmtpPassword"
      : "mailbox.newReceivePassword",
  );
  const submit = form.handleSubmit(async ({ password }) => {
    setError(undefined);
    const saved = await emit(
      "replace",
      { mailboxId: mailbox.id, purpose, password },
      {
        ephemeral: true,
        onError: (code) => setError(new ApiError(code, 400)),
      },
    );
    if (saved) onSaved();
  });
  return (
    <EditorDialog
      open
      title={label}
      description={mailbox.emailAddress}
      onOpenChange={(open) => {
        if (!open && !isSubmitting) onClose();
      }}
    >
      <form className="entity-form" noValidate onSubmit={submit}>
        <FormField label={label} error={errors.password?.message}>
          <Input
            type="password"
            autoComplete="new-password"
            maxLength={4096}
            disabled={isSubmitting}
            {...form.register("password")}
          />
        </FormField>
        {error !== undefined && <ErrorBanner error={error} />}
        <MailDialogActions
          busy={isSubmitting}
          label={t("mailbox.saveCredential")}
          onClose={onClose}
        />
      </form>
    </EditorDialog>
  );
}

type MailboxDialog =
  | { kind: "connect" }
  | { kind: "edit" | "smtp" | "pop3" | "receive"; record: Mailbox };

export function MailboxFormSlot({
  data,
  readOnly,
  emit,
}: ComponentRendererProps) {
  const dialog = data as MailboxDialog | null;
  if (readOnly || !dialog) return null;
  const close = () => {
    void emit("close");
  };
  const saved = async (feedback: "saved" | "verifyFailed") => {
    await emit("feedback", feedback);
    close();
    await emit("refresh");
  };
  if (dialog.kind === "connect" || dialog.kind === "edit") {
    return (
      <MailboxForm
        emit={emit}
        mailbox={dialog.kind === "edit" ? dialog.record : undefined}
        onClose={close}
        onSaved={(verified) => void saved(verified ? "saved" : "verifyFailed")}
      />
    );
  }
  if (
    dialog.kind === "smtp" ||
    dialog.kind === "pop3" ||
    dialog.kind === "receive"
  ) {
    const receive = dialog.record.channels.find(
      (channel) => channel.direction === "receive",
    );
    const purpose =
      dialog.kind === "smtp"
        ? "smtp"
        : receive?.protocol === "imap"
          ? "imap"
          : "pop3";
    return (
      <MailCredentialForm
        emit={emit}
        mailbox={dialog.record}
        purpose={purpose}
        onClose={close}
        onSaved={() => void saved("saved")}
      />
    );
  }
  return null;
}
