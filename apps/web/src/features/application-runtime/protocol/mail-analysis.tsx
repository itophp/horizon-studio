import { useId, useState, type ReactNode } from "react";
import { Sparkles } from "lucide-react";
import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import type {
  MailInboxRecommendOutput,
  MailMessageAnalyzeOutput,
} from "@/api/generated/capability-mail";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import type { ComponentRendererProps } from "./component-registry";

type AnalysisState =
  { status: "idle" | "pending" | "error" } | { status: "ready"; data: unknown };

export function MailAnalysis({
  event,
  emit,
}: {
  event: "recommend" | "analyze";
  emit: ComponentRendererProps["emit"];
}) {
  const { t, i18n } = useTranslation("mail");
  const [state, setState] = useState<AnalysisState>({ status: "idle" });
  const hint = useId();
  const recent = event === "recommend";

  async function analyze() {
    setState({ status: "pending" });
    const locale = i18n.resolvedLanguage?.startsWith("zh") ? "zh-CN" : "en";
    try {
      const succeeded = await emit(
        event,
        { locale },
        {
          ephemeral: true,
          onResult: (data) => setState({ status: "ready", data }),
          onError: () => setState({ status: "error" }),
        },
      );
      if (!succeeded) setState({ status: "error" });
    } catch {
      setState({ status: "error" });
    }
  }

  return (
    <section className="grid gap-3 text-sm">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <Button
          type="button"
          variant="outline"
          size="sm"
          aria-describedby={hint}
          disabled={state.status === "pending"}
          onClick={() => void analyze()}
        >
          {state.status === "pending" ? (
            <Spinner aria-hidden />
          ) : (
            <Sparkles aria-hidden />
          )}
          {t(recent ? "analysis.recommend" : "analysis.analyze")}
        </Button>
        <p id={hint} className="text-xs text-muted-foreground">
          {t(recent ? "analysis.recommendHint" : "analysis.analyzeHint")}
        </p>
      </div>
      {state.status === "pending" && (
        <p role="status">{t("analysis.pending")}</p>
      )}
      {state.status === "error" && (
        <p role="alert" className="text-destructive">
          {t("analysis.error")}
        </p>
      )}
      {state.status === "ready" && (
        <div className="grid gap-3 rounded-md border border-border bg-muted/30 p-3 wrap-anywhere">
          {recent ? (
            <Recommendations result={state.data as MailInboxRecommendOutput} />
          ) : (
            <MessageAnalysis result={state.data as MailMessageAnalyzeOutput} />
          )}
        </div>
      )}
    </section>
  );
}

function Recommendations({ result }: { result: MailInboxRecommendOutput }) {
  const { t } = useTranslation("mail");
  if (result.items.length === 0)
    return <p>{t("analysis.noRecommendations")}</p>;

  return (
    <ol className="grid gap-3">
      {result.items.map((item) => (
        <li key={item.id} className="grid gap-1">
          <Link
            to={`/mail/${item.id}`}
            className="w-fit font-medium text-primary underline underline-offset-2"
          >
            {item.subject || t("message.noSubject")}
          </Link>
          <p className="text-xs text-muted-foreground">
            {item.from || t("message.unknownSender")}
          </p>
          <p>{item.reason}</p>
        </li>
      ))}
    </ol>
  );
}

function MessageAnalysis({ result }: { result: MailMessageAnalyzeOutput }) {
  const { t } = useTranslation("mail");
  return (
    <>
      <div>
        <h2 className="font-semibold">{t("analysis.summary")}</h2>
        <p>{result.summary}</p>
      </div>
      <AnalysisList title={t("analysis.keyPoints")}>
        {result.keyPoints.map((point, index) => (
          <li key={index}>{point}</li>
        ))}
      </AnalysisList>
      <AnalysisList title={t("analysis.actionItems")}>
        {result.actionItems.map((item, index) => (
          <li key={index}>
            {item.task}
            {item.deadline && (
              <p className="text-muted-foreground">
                {t("analysis.actionDeadline", { deadline: item.deadline })}
              </p>
            )}
          </li>
        ))}
      </AnalysisList>
      <AnalysisList title={t("analysis.deadlines")}>
        {result.deadlines.map((deadline, index) => (
          <li key={index}>{deadline}</li>
        ))}
      </AnalysisList>
      {result.truncated && (
        <p className="text-xs text-muted-foreground">
          {t("analysis.truncated")}
        </p>
      )}
    </>
  );
}

function AnalysisList({
  title,
  children,
}: {
  title: string;
  children: ReactNode[];
}) {
  if (children.length === 0) return null;
  return (
    <div>
      <h2 className="font-semibold">{title}</h2>
      <ul className="list-disc ps-5">{children}</ul>
    </div>
  );
}
