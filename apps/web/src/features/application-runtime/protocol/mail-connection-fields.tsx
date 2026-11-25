import { useEffect, useState } from "react";
import { Controller, useWatch, type UseFormReturn } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import { FormField } from "@/components/shared/form-field";
import { Input } from "@/components/ui/input";
import {
  NativeSelect,
  NativeSelectOption,
} from "@/components/ui/native-select";
import type { Mailbox } from "@/lib/capabilities";

const host = z.string().trim().min(1, "required");
const port = z
  .number({ error: "port" })
  .int("port")
  .min(1, "port")
  .max(65535, "port");
const smtpPorts = { starttls: 587, tls: 465 };
const receiveServers = {
  pop3: { prefix: "pop", tls: 995, starttls: 995 },
  imap: { prefix: "imap", tls: 993, starttls: 143 },
};
type SuggestedField =
  "smtpHost" | "smtpUsername" | "receiveHost" | "receiveUsername";

export const mailboxSchema = z.object({
  name: z.string().trim().min(1, "required"),
  emailAddress: z.email("email"),
  password: z.string(),
  smtpHost: host,
  smtpPort: port,
  smtpTls: z.enum(["starttls", "tls"]),
  smtpUsername: z.string().trim(),
  receiveProtocol: z.enum(["pop3", "imap"]),
  receiveHost: host,
  receivePort: port,
  receiveTls: z.enum(["tls", "starttls"]),
  receiveUsername: z.string().trim(),
});

function hostFor(email: string, prefix: string) {
  const domain = email.split("@")[1]?.toLowerCase().trim();
  return domain ? `${prefix}.${domain}` : "";
}

export function serverSettings(mailbox: Mailbox | undefined) {
  const smtp = mailbox?.channels.find(
    (channel) => channel.direction === "send",
  );
  const receive = mailbox?.channels.find(
    (channel) => channel.direction === "receive",
  );
  const email = mailbox?.emailAddress ?? "";
  const smtpTls = smtp?.tlsMode ?? "starttls";
  const receiveProtocol = receive?.protocol === "imap" ? "imap" : "pop3";
  const receiveTls =
    receiveProtocol === "imap" ? (receive?.tlsMode ?? "tls") : "tls";
  return {
    smtpHost: smtp?.host ?? hostFor(email, "smtp"),
    smtpPort: smtp?.port ?? smtpPorts[smtpTls],
    smtpTls,
    smtpUsername: smtp?.username ?? email,
    receiveProtocol,
    receiveTls,
    receiveHost:
      receive?.host ?? hostFor(email, receiveServers[receiveProtocol].prefix),
    receivePort: receive?.port ?? receiveServers[receiveProtocol][receiveTls],
    receiveUsername: receive?.username ?? email,
  } as const;
}

export function MailConnectionFields({
  form,
  connecting,
}: {
  form: UseFormReturn<z.infer<typeof mailboxSchema>>;
  connecting: boolean;
}) {
  const { t } = useTranslation("mail");
  const email = useWatch({ control: form.control, name: "emailAddress" });
  const protocol = useWatch({ control: form.control, name: "receiveProtocol" });
  const imap = protocol === "imap";
  const { errors } = form.formState;
  const [edited, setEdited] = useState<Partial<Record<SuggestedField, true>>>(
    {},
  );
  const registerSuggestedField = (field: SuggestedField) =>
    form.register(field, {
      onChange: () =>
        setEdited((previous) =>
          previous[field] ? previous : { ...previous, [field]: true },
        ),
    });

  useEffect(() => {
    if (!connecting) return;
    for (const [field, value] of [
      ["smtpHost", hostFor(email, "smtp")],
      ["smtpUsername", email],
      ["receiveHost", hostFor(email, receiveServers[protocol].prefix)],
      ["receiveUsername", email],
    ] as const) {
      if (!edited[field]) form.setValue(field, value);
    }
  }, [connecting, edited, email, form, protocol]);

  return (
    <>
      <fieldset>
        <legend className="mb-3 text-sm font-medium">
          {t("mailbox.smtpSettings")}
        </legend>
        <div className="editor-grid">
          <FormField
            label={t("mailbox.smtpHost")}
            error={errors.smtpHost?.message}
          >
            <Input maxLength={253} {...registerSuggestedField("smtpHost")} />
          </FormField>
          <FormField
            label={t("mailbox.smtpPort")}
            error={errors.smtpPort?.message}
          >
            <Input
              type="number"
              min={1}
              max={65535}
              step={1}
              {...form.register("smtpPort", { valueAsNumber: true })}
            />
          </FormField>
          <FormField
            label={t("mailbox.smtpUsername")}
            error={errors.smtpUsername?.message}
          >
            <Input
              maxLength={200}
              {...registerSuggestedField("smtpUsername")}
            />
          </FormField>
          <Controller
            control={form.control}
            name="smtpTls"
            render={({ field, fieldState }) => (
              <FormField
                label={t("mailbox.smtpEncryption")}
                error={fieldState.error?.message}
              >
                <NativeSelect
                  className="w-full"
                  {...field}
                  onChange={(event) => {
                    const mode =
                      event.currentTarget.value === "tls" ? "tls" : "starttls";
                    if (
                      connecting &&
                      form.getValues("smtpPort") === smtpPorts[field.value]
                    ) {
                      form.setValue("smtpPort", smtpPorts[mode], {
                        shouldValidate: true,
                      });
                    }
                    field.onChange(mode);
                  }}
                >
                  <NativeSelectOption value="starttls">
                    {t("mailbox.startTls")}
                  </NativeSelectOption>
                  <NativeSelectOption value="tls">
                    {t("mailbox.implicitTls")}
                  </NativeSelectOption>
                </NativeSelect>
              </FormField>
            )}
          />
        </div>
      </fieldset>
      <fieldset>
        <legend className="mb-3 text-sm font-medium">
          {t("mailbox.receiveSettings")}
        </legend>
        <div className="editor-grid">
          <Controller
            control={form.control}
            name="receiveProtocol"
            render={({ field, fieldState }) => (
              <FormField
                label={t("mailbox.receiveProtocol")}
                error={fieldState.error?.message}
                wide
              >
                <NativeSelect
                  className="w-full"
                  {...field}
                  onChange={({ currentTarget }) => {
                    const next =
                      currentTarget.value === "imap" ? "imap" : "pop3";
                    const previous = receiveServers[field.value];
                    const currentHost = form.getValues("receiveHost");
                    if (
                      !currentHost ||
                      currentHost === hostFor(email, previous.prefix)
                    ) {
                      form.setValue(
                        "receiveHost",
                        hostFor(email, receiveServers[next].prefix),
                      );
                    }
                    if (
                      form.getValues("receivePort") ===
                      previous[form.getValues("receiveTls")]
                    ) {
                      form.setValue("receivePort", receiveServers[next].tls, {
                        shouldValidate: true,
                      });
                    }
                    form.setValue("receiveTls", "tls");
                    field.onChange(next);
                  }}
                >
                  <NativeSelectOption value="pop3">
                    {t("mailbox.popProtocol")}
                  </NativeSelectOption>
                  <NativeSelectOption value="imap">
                    {t("mailbox.imapProtocol")}
                  </NativeSelectOption>
                </NativeSelect>
              </FormField>
            )}
          />
          <FormField
            label={t(imap ? "mailbox.imapHost" : "mailbox.popHost")}
            error={errors.receiveHost?.message}
          >
            <Input maxLength={253} {...registerSuggestedField("receiveHost")} />
          </FormField>
          <FormField
            label={t(imap ? "mailbox.imapPort" : "mailbox.popPort")}
            error={errors.receivePort?.message}
          >
            <Input
              type="number"
              min={1}
              max={65535}
              step={1}
              {...form.register("receivePort", { valueAsNumber: true })}
            />
          </FormField>
          <FormField
            label={t(imap ? "mailbox.imapUsername" : "mailbox.popUsername")}
            error={errors.receiveUsername?.message}
          >
            <Input
              maxLength={200}
              {...registerSuggestedField("receiveUsername")}
            />
          </FormField>
          {imap ? (
            <Controller
              control={form.control}
              name="receiveTls"
              render={({ field, fieldState }) => (
                <FormField
                  label={t("mailbox.imapEncryption")}
                  error={fieldState.error?.message}
                >
                  <NativeSelect
                    className="w-full"
                    {...field}
                    onChange={({ currentTarget }) => {
                      const mode =
                        currentTarget.value === "starttls" ? "starttls" : "tls";
                      if (
                        form.getValues("receivePort") ===
                        receiveServers.imap[field.value]
                      ) {
                        form.setValue(
                          "receivePort",
                          receiveServers.imap[mode],
                          { shouldValidate: true },
                        );
                      }
                      field.onChange(mode);
                    }}
                  >
                    <NativeSelectOption value="tls">
                      {t("mailbox.implicitTls")}
                    </NativeSelectOption>
                    <NativeSelectOption value="starttls">
                      {t("mailbox.startTls")}
                    </NativeSelectOption>
                  </NativeSelect>
                </FormField>
              )}
            />
          ) : (
            <FormField label={t("mailbox.popEncryption")}>
              <Input value={t("mailbox.implicitTls")} readOnly />
            </FormField>
          )}
        </div>
      </fieldset>
    </>
  );
}
