import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError } from "@/api/client/request";
import type { RuntimeAIFormAssistant } from "@/api/generated/runtime";
import { AIFormAssistant } from "./ai-form-assistant";
import type { ComponentRendererProps } from "./component-registry";

const assistant: RuntimeAIFormAssistant = {
  labelKey: "mail:compose.aiDraft",
  action: "suggest-draft",
  inputFields: ["brief"],
  targetFields: ["to", "cc", "subject", "body"],
  prompt: "",
};

const formFields = ["to", "cc", "subject"] as const;
type DraftField = (typeof formFields)[number];
const isDraftField = (field: string): field is DraftField =>
  formFields.some((name) => name === field);

export function MailComposeAssistant({
  emit,
  disabled,
  values,
  html,
  getValue,
  getHTML,
  setValue,
  insertBody,
}: {
  emit: ComponentRendererProps["emit"];
  disabled: boolean;
  values: Record<DraftField, string>;
  html: string;
  getValue: (field: DraftField) => string;
  getHTML: () => string;
  setValue: (field: DraftField, value: string) => void;
  insertBody: (value: string) => void;
}) {
  const { t, i18n } = useTranslation("mail");
  const [brief, setBrief] = useState("");

  return (
    <AIFormAssistant
      spec={assistant}
      showLabel
      allowAttachments={false}
      hint={t("compose.aiDraftHint")}
      draftSource={{
        label: t("compose.aiBrief"),
        value: brief,
        maxLength: 2000,
        onChange: setBrief,
      }}
      request={async () => {
        let data: unknown;
        let errorCode = "unavailable";
        const succeeded = await emit(
          assistant.action,
          {
            sourceText: brief.trim(),
            includeSubject: !getValue("subject").trim(),
            includeTo: !getValue("to").trim(),
            includeCc: !getValue("cc").trim(),
            locale: i18n.resolvedLanguage?.startsWith("zh") ? "zh-CN" : "en",
          },
          {
            ephemeral: true,
            onResult: (value) => {
              data = value;
            },
            onError: (code) => {
              errorCode = code;
            },
          },
        );
        if (!succeeded) throw new ApiError(errorCode, 400);
        return data;
      }}
      current={(field) => {
        if (field === "brief") return brief;
        if (isDraftField(field)) return values[field];
        return html ? t("compose.aiExistingBody") : "";
      }}
      currentValue={(field) =>
        isDraftField(field) ? getValue(field) : getHTML()
      }
      label={(field) =>
        t(isDraftField(field) ? `compose.${field}` : "compose.body")
      }
      canApply={({ field }) =>
        !disabled &&
        (field === "body" || (isDraftField(field) && !getValue(field).trim()))
      }
      apply={({ field, value }) => {
        if (isDraftField(field)) setValue(field, value);
        if (field === "body") insertBody(value);
      }}
      disabled={disabled}
    />
  );
}
