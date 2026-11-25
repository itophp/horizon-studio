import { useTranslation } from "react-i18next";
import { Check, FilterX } from "lucide-react";
import type { IdentityAuditListInput } from "@/api/generated/capability-identity";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { EditorDialog } from "./shared";

export type AuditFilters = Pick<
  IdentityAuditListInput,
  | "action"
  | "outcome"
  | "origin"
  | "resourceId"
  | "correlationId"
  | "principalId"
  | "bindingId"
  | "delegationId"
>;

const uuid =
  "[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}";

export function SecurityAuditFilters({
  initial,
  onApply,
  onClose,
}: {
  initial: AuditFilters;
  onApply: (filters: AuditFilters) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const fields: {
    key: keyof AuditFilters;
    label: string;
    length: number;
    uuid?: boolean;
  }[] = [
    { key: "action", label: t("admin:security.audit.action"), length: 200 },
    { key: "outcome", label: t("admin:security.audit.outcome"), length: 64 },
    { key: "origin", label: t("admin:security.audit.origin"), length: 64 },
    {
      key: "resourceId",
      label: t("admin:security.audit.resourceId"),
      length: 256,
    },
    {
      key: "correlationId",
      label: t("admin:security.audit.correlation"),
      length: 256,
    },
    {
      key: "principalId",
      label: t("admin:review.principal"),
      length: 36,
      uuid: true,
    },
    {
      key: "bindingId",
      label: t("admin:security.audit.bindingId"),
      length: 36,
      uuid: true,
    },
    {
      key: "delegationId",
      label: t("admin:security.audit.delegationId"),
      length: 36,
      uuid: true,
    },
  ];
  return (
    <EditorDialog
      open
      title={t("admin:security.audit.filterTitle")}
      description={t("admin:security.audit.filterHelp")}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <form
        className="entity-form"
        onSubmit={(event) => {
          event.preventDefault();
          const form = new FormData(event.currentTarget);
          const filters: AuditFilters = {};
          for (const { key } of fields) {
            const value = String(form.get(key) ?? "").trim();
            if (value) filters[key] = value;
          }
          onApply(filters);
        }}
      >
        {fields.map((field) => (
          <label className="form-field" key={field.key}>
            <span>{field.label}</span>
            <Input
              name={field.key}
              defaultValue={initial[field.key] ?? ""}
              maxLength={field.length}
              pattern={field.uuid ? uuid : undefined}
            />
          </label>
        ))}
        <div className="form-actions">
          <Button type="button" variant="outline" onClick={() => onApply({})}>
            <FilterX />
            {t("admin:actions.clear")}
          </Button>
          <Button type="submit">
            <Check />
            {t("admin:actions.filter")}
          </Button>
        </div>
      </form>
    </EditorDialog>
  );
}
