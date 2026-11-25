import { useTranslation } from "react-i18next";
import type { IdentityPolicyCheckOutput } from "@/api/generated/capability-identity";

export function PolicyIssues({
  issues,
}: {
  issues: IdentityPolicyCheckOutput["issues"];
}) {
  const { t } = useTranslation();
  if (!issues.length) return <p role="status">{t("admin:policy.noIssues")}</p>;
  const labels: Record<string, string> = {
    invalid: t("admin:policy.invalidRules"),
    conflict: t("admin:policy.ruleConflict"),
    unknown_action_resource: t("admin:policy.unknownAction"),
    row_filter_unavailable: t("admin:policy.missingFilter"),
    output_mask_unavailable: t("admin:policy.missingMask"),
    attribute_unavailable: t("admin:policy.missingAttribute"),
  };
  return (
    <ul className="list-disc space-y-1 pl-5" aria-live="polite">
      {issues.map((issue) => (
        <li key={issue.rule}>
          {issue.rule
            ? t("admin:policy.issueRow", {
                row: issue.rule,
                issue: labels[issue.code] ?? labels.invalid,
              })
            : (labels[issue.code] ?? labels.invalid)}
        </li>
      ))}
    </ul>
  );
}
