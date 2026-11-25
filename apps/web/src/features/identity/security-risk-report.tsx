import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import { RefreshCw } from "lucide-react";
import type { IdentityGovernanceReportOutput } from "@/api/generated/capability-identity";
import { identityGovernanceReport } from "@/lib/capabilities";
import { formatNumber } from "@/lib/format";
import { RiskAccountSignals } from "./risk-account-signals";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  DataTable,
  ErrorBanner,
  Loading,
  ShortDate,
  type AdminColumn,
} from "./shared";

type Report = IdentityGovernanceReportOutput;
type GrantRow = Omit<Report["privilegedGrants"][number], "id"> & {
  id: number;
  grantId: string;
};
type DenialRow = Report["repeatedDenials"][number] & { id: number };

export function SecurityRiskReport({
  queryKey,
}: {
  queryKey: readonly unknown[];
}) {
  const { t, i18n } = useTranslation();
  const [params, setParams] = useSearchParams();
  const windowHours = params.get("riskHours") === "168" ? 168 : 24;
  const report = useQuery({
    queryKey: [...queryKey, "risk", windowHours],
    queryFn: ({ signal }) => identityGovernanceReport({ windowHours }, signal),
    retry: false,
  });
  const grantColumns: AdminColumn<GrantRow>[] = [
    {
      id: "member",
      header: t("admin:risk.member"),
      cell: (row) => <span>{row.username}</span>,
    },
    { id: "role", header: t("admin:risk.role"), cell: (row) => row.roleName },
    {
      id: "expiry",
      header: t("admin:risk.expires"),
      cell: (row) => <ShortDate value={row.expiresAt} />,
    },
  ];
  const denialColumns: AdminColumn<DenialRow>[] = [
    {
      id: "action",
      header: t("admin:security.audit.action"),
      cell: (row) => row.action,
    },
    {
      id: "reason",
      header: t("admin:security.audit.reason"),
      cell: (row) => row.reason,
    },
    {
      id: "occurrences",
      header: t("admin:risk.occurrences"),
      cell: (row) => formatNumber(row.occurrences, i18n.language),
    },
    {
      id: "accounts",
      header: t("admin:risk.accounts"),
      cell: (row) => formatNumber(row.affectedAccounts, i18n.language),
    },
  ];
  const data = report.data?.data;
  // Numeric IDs belong to the read-only table; preserve the real grant ID
  // separately so a future action cannot mistake a row index for a grant.
  const grantRows = (data?.privilegedGrants ?? []).map((row, index) => ({
    ...row,
    grantId: row.id,
    id: index + 1,
  }));
  const denialRows = (data?.repeatedDenials ?? []).map((row, index) => ({
    ...row,
    id: index + 1,
  }));
  return (
    <div className="space-y-4">
      <p className="muted-text">{t("admin:risk.help")}</p>
      <div className="standalone-toolbar flex-wrap">
        <Select
          value={String(windowHours)}
          onValueChange={(value) =>
            setParams((previous) => {
              const next = new URLSearchParams(previous);
              next.set("riskHours", value === "168" ? "168" : "24");
              return next;
            })
          }
        >
          <SelectTrigger aria-label={t("admin:risk.window")}>
            <SelectValue>
              {t(windowHours === 168 ? "admin:risk.week" : "admin:risk.day")}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="24">{t("admin:risk.day")}</SelectItem>
            <SelectItem value="168">{t("admin:risk.week")}</SelectItem>
          </SelectContent>
        </Select>
        <Button
          type="button"
          variant="outline"
          disabled={report.isFetching}
          onClick={() => void report.refetch()}
        >
          <RefreshCw />
          {t("admin:actions.refresh")}
        </Button>
      </div>
      {report.isPending ? (
        <Loading />
      ) : report.error ? (
        <ErrorBanner
          error={report.error}
          onRetry={() => void report.refetch()}
        />
      ) : (
        data && (
          <>
            <p className="muted-text">
              {t("admin:risk.asOf")} <ShortDate value={data.asOf} />
            </p>
            <dl className="grid gap-4 sm:grid-cols-3">
              <div>
                <dt className="muted-text">{t("admin:risk.authentication")}</dt>
                <dd className="text-2xl font-semibold">
                  {formatNumber(data.authenticationFailures, i18n.language)}
                </dd>
              </div>
              <div>
                <dt className="muted-text">{t("admin:risk.denials")}</dt>
                <dd className="text-2xl font-semibold">
                  {formatNumber(data.authorizationDenials, i18n.language)}
                </dd>
              </div>
              <div>
                <dt className="muted-text">{t("admin:risk.privileged")}</dt>
                <dd className="text-2xl font-semibold">
                  {formatNumber(data.privilegedGrantTotal, i18n.language)}
                </dd>
              </div>
            </dl>
            <section
              className="space-y-2"
              aria-label={t("admin:risk.privileged")}
            >
              <h2 className="font-semibold">{t("admin:risk.privileged")}</h2>
              <p className="muted-text">{t("admin:risk.grantsHelp")}</p>
              <DataTable rows={grantRows} columns={grantColumns} serverPage />
            </section>
            <section
              className="space-y-2"
              aria-label={t("admin:risk.repeated")}
            >
              <h2 className="font-semibold">{t("admin:risk.repeated")}</h2>
              <p className="muted-text">{t("admin:risk.denialsHelp")}</p>
              <DataTable rows={denialRows} columns={denialColumns} serverPage />
            </section>
            <RiskAccountSignals data={data} />
          </>
        )
      )}
    </div>
  );
}
