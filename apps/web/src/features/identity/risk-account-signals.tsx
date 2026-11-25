import { useTranslation } from "react-i18next";
import type { IdentityGovernanceReportOutput } from "@/api/generated/capability-identity";
import { formatNumber } from "@/lib/format";
import { DataTable, ShortDate, type AdminColumn } from "./shared";

type Report = IdentityGovernanceReportOutput;
type Authentication = NonNullable<Report["repeatedAuthentication"]>[number] & {
  id: number;
};

export function RiskAccountSignals({ data }: { data: Report }) {
  const { t, i18n } = useTranslation();
  const account = (row: Authentication) => <span>{row.username}</span>;
  const authentication: AdminColumn<Authentication>[] = [
    { id: "account", header: t("admin:risk.member"), cell: account },
    {
      id: "failures",
      header: t("admin:risk.occurrences"),
      cell: (row) => formatNumber(row.occurrences, i18n.language),
    },
    {
      id: "lastAt",
      header: t("admin:risk.lastFailure"),
      cell: (row) => <ShortDate value={row.lastAt} />,
    },
  ];
  return (
    <>
      {data.repeatedAuthentication && (
        <section className="space-y-2">
          <h2 className="font-semibold">
            {t("admin:risk.repeatedAuthentication")}
          </h2>
          <p className="muted-text">
            {t("admin:risk.repeatedAuthenticationHelp")}
          </p>
          <DataTable
            columns={authentication}
            rows={data.repeatedAuthentication.map((row, index) => ({
              ...row,
              id: index + 1,
            }))}
            serverPage
          />
        </section>
      )}
    </>
  );
}
