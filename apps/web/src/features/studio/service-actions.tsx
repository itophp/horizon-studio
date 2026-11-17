import {
  ArrowUpRight,
  CirclePlay,
  LoaderCircle,
  RefreshCw,
  Square,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import type { ProjectService } from "./api";
import { runningServiceURL } from "./service-status";

export type ServiceAction = "start" | "restart" | "stop";

export function ServiceActions({
  runningServices,
  canStart,
  initialized,
  pending,
  activate,
}: {
  runningServices: readonly ProjectService[];
  canStart: boolean;
  initialized: boolean;
  pending: ServiceAction | null;
  activate: (action: ServiceAction) => Promise<void>;
}) {
  const { t } = useTranslation("studio");
  if (canStart) {
    return (
      <Button
        type="button"
        size="sm"
        className="rounded-full px-3"
        title={initialized ? undefined : t("initializationRequired")}
        disabled={pending !== null || !initialized}
        onClick={() => void activate("start")}
      >
        {pending === "start" ? (
          <LoaderCircle className="motion-safe:animate-spin" />
        ) : (
          <CirclePlay />
        )}
        {t("start")}
      </Button>
    );
  }
  if (runningServices.length === 0) return null;

  const webURL = runningServiceURL(runningServices, "frontend");
  const apiURL = runningServiceURL(runningServices, "backend", "/api/health");
  const href = webURL ?? apiURL;
  return (
    <>
      {href && (
        <a
          className={buttonVariants({
            size: "sm",
            className: "rounded-full px-3",
          })}
          href={href}
          target="_blank"
          rel="noopener noreferrer"
          title={t(webURL ? "visitFrontend" : "visitBackend")}
        >
          {t("visit")}
          <ArrowUpRight className="size-3.5" />
        </a>
      )}
      <div className="flex shrink-0 items-center gap-0.5 rounded-full bg-background/70 p-0.5">
        <TooltipProvider delay={300}>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type="button"
                  size="icon-sm"
                  variant="ghost"
                  className="rounded-full text-muted-foreground"
                  aria-label={t("restart")}
                  disabled={pending !== null || !initialized}
                  onClick={() => void activate("restart")}
                />
              }
            >
              {pending === "restart" ? (
                <LoaderCircle className="motion-safe:animate-spin" />
              ) : (
                <RefreshCw />
              )}
            </TooltipTrigger>
            <TooltipContent side="bottom">
              {t(initialized ? "restart" : "initializationRequired")}
            </TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type="button"
                  size="icon-sm"
                  variant="ghost"
                  className="rounded-full text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                  aria-label={t("stop")}
                  disabled={pending !== null}
                  onClick={() => void activate("stop")}
                />
              }
            >
              {pending === "stop" ? (
                <LoaderCircle className="motion-safe:animate-spin" />
              ) : (
                <Square />
              )}
            </TooltipTrigger>
            <TooltipContent side="bottom">{t("stop")}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </div>
    </>
  );
}
