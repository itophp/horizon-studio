import { ApplicationDesignPage } from "@/features/application-runtime/design-page";
import { DynamicApplicationRoute } from "@/features/application-runtime/route";
import { frontendExtensions } from "@/app/plugins/builtins";
import { InstallationsPage } from "@/features/application-runtime/installations";

import { AiPage } from "@/features/ai/page";
import {
  AutomationView,
  LegacyAutomationRedirect,
} from "@/features/ai/automation-view";
import { KnowledgePage } from "@/features/knowledge/page";
import { KnowledgeDetailPage } from "@/features/knowledge/detail";
import "@/features/capabilities/capability.css";
import { FilesPage, FileDownloadPage } from "@/features/governance/files";
import {
  NotificationsPage,
  NotificationDetailPage,
} from "@/features/governance/notifications";
import { NotificationComposePage } from "@/features/governance/notification-compose";
import { TemporaryAccessPage } from "@/features/identity/temporary-access";
import {
  AuthBoundary,
  RequireSession,
  AdminLayout,
} from "@/features/identity/layout";
import { LoginPage } from "@/features/identity/login";
import { FederationCallbackPage } from "@/features/identity/federation-callback";
import { useSession } from "@/features/identity/auth";
import { sessionLandingPath } from "@/features/identity/session-navigation";
import { createBrowserRouter, Navigate, useSearchParams } from "react-router";
import { lazy, Suspense } from "react";
import { useTranslation } from "react-i18next";
import { Loading } from "@/features/identity/shared";
import { Shell } from "../layouts/shell";
import { studioProjectsPath } from "@/features/studio/routes";
import { Overview } from "@/features/example/overview";
// The graph canvas pulls in React Flow and dagre, so the orchestration page is
// split out instead of inflating the first screen bundle.
const CapabilityPlansPage = lazy(() =>
  import("@/features/execution-plans/page").then((module) => ({
    default: module.CapabilityPlansPage,
  })),
);
const OrchestrationPage = lazy(() =>
  import("@/features/orchestration/page").then((module) => ({
    default: module.OrchestrationPage,
  })),
);
const StudioPage = lazy(() =>
  import("@/features/studio/page").then((module) => ({
    default: module.StudioPage,
  })),
);
const StudioProjectManagementPage = lazy(() =>
  import("@/features/studio/project-management").then((module) => ({
    default: module.StudioProjectManagementPage,
  })),
);
function Architecture() {
  const { t } = useTranslation();
  return (
    <section className="page-intro">
      <h1>{t("about.title")}</h1>
      <p>{t("about.description")}</p>
      <div className="panel prose">
        <p>{t("about.contract")}</p>
        <p>{t("about.auth")}</p>
        <p>{t("about.generated")}</p>
      </div>
    </section>
  );
}
function NotFound() {
  const { t } = useTranslation();
  return <h1>{t("notFound")}</h1>;
}
// The AI workbench is the default surface after sign in. An account without the
// agent grant lands on statistics instead of a workbench it may not open.
export function defaultLandingPath(permissions: readonly string[]) {
  return permissions.includes("assistant:use") ? "/ai" : "/analytics";
}
function DefaultLanding() {
  const session = useSession();
  return (
    <Navigate
      to={sessionLandingPath(
        session.portal,
        defaultLandingPath(session.permissions),
      )}
      replace
    />
  );
}
function LegacyDesignRedirect() {
  const [params] = useSearchParams();
  const application = params.get("application");
  return (
    <Navigate
      to={
        application
          ? `/applications/design/${encodeURIComponent(application)}`
          : "/applications"
      }
      replace
    />
  );
}

function WorkbenchRoute() {
  const [params] = useSearchParams();
  return params.get("tab") === "automation" ? (
    <LegacyAutomationRedirect />
  ) : (
    <AiPage />
  );
}

export const router = createBrowserRouter([
  {
    element: <AuthBoundary />,
    children: [
      { path: "/login", element: <LoginPage /> },
      { path: "/sign-in/:application", element: <LoginPage /> },
      { path: "/login/oidc", element: <FederationCallbackPage /> },
      {
        path: "/login/saml",
        element: <FederationCallbackPage protocol="saml" />,
      },
      {
        element: <RequireSession />,
        children: [
          {
            path: "/studio",
            element: (
              <Suspense fallback={<Loading />}>
                <StudioPage />
              </Suspense>
            ),
          },
          {
            element: <AdminLayout />,
            children: [
              { path: "/", element: <DefaultLanding /> },
              { path: "/applications", element: <InstallationsPage /> },
              {
                path: `${studioProjectsPath}/:projectId?`,
                element: (
                  <Suspense fallback={<Loading />}>
                    <StudioProjectManagementPage />
                  </Suspense>
                ),
              },
              {
                path: "/applications/design",
                element: <LegacyDesignRedirect />,
              },
              {
                path: "/applications/design/:applicationId",
                element: <ApplicationDesignPage />,
              },
              { path: "*", element: <DynamicApplicationRoute /> },
              { path: "/analytics", element: <DynamicApplicationRoute /> },
              { path: "/capabilities", element: <DynamicApplicationRoute /> },
              {
                path: "/capabilities/:name",
                element: (
                  <Suspense fallback={<Loading />}>
                    <CapabilityPlansPage />
                  </Suspense>
                ),
              },
              {
                path: "/orchestration",
                element: (
                  <Suspense fallback={<Loading />}>
                    <OrchestrationPage />
                  </Suspense>
                ),
              },
              { path: "/ai", element: <WorkbenchRoute /> },
              { path: "/ai/automation", element: <AutomationView /> },
              { path: "/ai/automation/:id", element: <AutomationView /> },
              {
                path: "/ai/automation/runs/:runId",
                element: <AutomationView />,
              },
              { path: "/automation", element: <LegacyAutomationRedirect /> },
              {
                path: "/automation/:id",
                element: <LegacyAutomationRedirect />,
              },
              {
                path: "/automation/runs/:runId",
                element: <LegacyAutomationRedirect />,
              },
              ...frontendExtensions.routes,
              { path: "/knowledge", element: <KnowledgePage /> },
              { path: "/knowledge/:id", element: <KnowledgeDetailPage /> },
              {
                path: "/departments",
                element: (
                  <Navigate to="/organizations?tab=departments" replace />
                ),
              },
              {
                path: "/roles",
                element: <Navigate to="/permissions?type=roles" replace />,
              },
              { path: "/settings", element: <DynamicApplicationRoute /> },
              { path: "/mcp", element: <DynamicApplicationRoute /> },
              {
                path: "/mcp-settings",
                element: <Navigate to="/mcp?tab=remote" replace />,
              },
              {
                path: "/webmcp",
                element: <Navigate to="/mcp?tab=page" replace />,
              },
              { path: "/preferences", element: <DynamicApplicationRoute /> },
              {
                path: "/dictionaries",
                element: <Navigate to="/settings?tab=fields" replace />,
              },
              { path: "/attachments", element: <FilesPage /> },
              { path: "/files/:id", element: <FileDownloadPage /> },
              { path: "/notifications", element: <NotificationsPage /> },
              {
                path: "/notifications/compose",
                element: <NotificationComposePage />,
              },
              {
                path: "/notifications/:id",
                element: <NotificationDetailPage />,
              },
              { path: "/temporary-access", element: <TemporaryAccessPage /> },
            ],
          },
        ],
      },
    ],
  },
  {
    element: <Shell />,
    children: [
      { path: "/foundation", element: <Overview /> },
      { path: "/architecture", element: <Architecture /> },
      { path: "*", element: <NotFound /> },
    ],
  },
]);
