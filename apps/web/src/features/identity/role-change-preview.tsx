import type { IdentityRolePreviewOutput } from "@/api/generated/capability-identity";
import { useTranslation } from "react-i18next";

export function RoleChangePreview({
  result,
}: {
  result: IdentityRolePreviewOutput;
}) {
  const { t } = useTranslation();
  return (
    <section
      className="space-y-2 rounded-md border p-3"
      aria-label={t("admin:roleReview.preview")}
    >
      <p>{t("admin:roleReview.impact", result.impact)}</p>
      {result.statusBefore !== result.statusAfter && (
        <p>
          {t("admin:roleReview.statusChange", {
            before: t(
              result.statusBefore === "active"
                ? "admin:status.active"
                : "admin:status.inactive",
            ),
            after: t(
              result.statusAfter === "active"
                ? "admin:status.active"
                : "admin:status.inactive",
            ),
          })}
        </p>
      )}
      {(["added", "removed"] as const).map((kind) => (
        <div key={kind}>
          <h3 className="font-medium">
            {t(
              kind === "added"
                ? "admin:roleReview.added"
                : "admin:roleReview.removed",
            )}
          </h3>
          {result[kind].length > 0 ? (
            <ul className="max-h-36 overflow-auto">
              {result[kind].map((grant) => (
                <li key={JSON.stringify(grant)}>
                  {t("admin:roleReview.grant", grant)}
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-muted-foreground">
              {t("admin:roleReview.noChanges")}
            </p>
          )}
        </div>
      ))}
    </section>
  );
}
