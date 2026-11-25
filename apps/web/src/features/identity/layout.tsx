import { studioProjectsPath } from "@/features/studio/routes";
import { useApplicationCatalog } from "@/features/application-runtime/catalog";
import { WorkspaceHost } from "@/features/application-runtime/workspace";
import { canShowMenu } from "@/app/plugins/registry";
import { WebMcpProvider } from "@/features/webmcp/provider";
import { AssistantProvider } from "@/features/assistant/provider";
import { AssistantLauncher } from "@/features/assistant/widget";
import { PublishedLogo, useBranding } from "@/features/governance/branding";
import { NotificationBell } from "@/features/governance/notifications";
import { useConfiguration } from "@/features/governance/runtime";
import { RuntimeProvider } from "@/features/governance/runtime";
import { useEffect, useRef, useState } from "react";
import {
  Navigate,
  NavLink,
  Outlet,
  Link,
  useLocation,
  useNavigate,
} from "react-router";
import { useTranslation } from "react-i18next";
import {
  LayoutDashboard,
  Clock,
  Users,
  Building2,
  Network,
  ShieldCheck,
  KeyRound,
  PanelLeft,
  Search,
  Sun,
  Moon,
  ChevronDown,
  LogOut,
  UserRound,
  ArrowUpRight,
  X,
  Command,
  Settings2,
  SlidersHorizontal,
  Sparkles,
  BookOpen,
  Boxes,
  Workflow,
} from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@/components/ui/dropdown-menu";
import { Toaster } from "@/components/ui/sonner";
import { buttonVariants } from "@/components/ui/button";
import { changeLanguage } from "@/i18n";
import { useTheme } from "@/app/providers/theme";
import { AuthProvider, useAuth, useSession } from "./auth";
import { ActivityCapture } from "./activity-capture";
import { recordNavigation } from "@/lib/activity";
import {
  BrandMark,
  EditorDialog,
  initials,
  Loading,
  useAdminData,
  StatusBadge,
  ErrorBanner,
} from "./shared";
export function AuthBoundary() {
  return (
    <AuthProvider>
      <Outlet />
      <Toaster />
    </AuthProvider>
  );
}
export function RequireSession() {
  const location = useLocation();
  const auth = useAuth();
  if (auth.loading)
    return (
      <div className="auth-loading-page">
        <Loading />
      </div>
    );
  if (!auth.profile)
    return (
      <Navigate
        to="/login"
        state={{ returnTo: location.pathname + location.search }}
        replace
      />
    );
  // The floating helper and the AI workbench drive the same backend agent, so
  // the helper stays out of the way while the workbench itself is open.
  const onAgentPage = /^\/(ai|studio)(\/|$)/.test(location.pathname);
  return (
    <RuntimeProvider>
      <WebMcpProvider
        key={`${auth.profile.tenantId}:${auth.profile.account.id}:${auth.profile.identityBindingId}:${auth.profile.csrfToken}`}
      >
        <AssistantProvider>
          <ActivityCapture
            key={`${auth.profile.tenantId}:${auth.profile.account.id}:${auth.profile.identityBindingId}`}
            enabled={auth.profile.permissions.includes("memory:collect")}
          />
          <Outlet />
          {!onAgentPage && <AssistantLauncher />}
        </AssistantProvider>
      </WebMcpProvider>
    </RuntimeProvider>
  );
}
export function AdminLayout() {
  const { t, i18n } = useTranslation();
  const config = useConfiguration();
  const brand = useBranding();
  const session = useSession();
  const auth = useAuth();
  const navigate = useNavigate();
  const { theme, toggle } = useTheme();
  const { data } = useAdminData();
  const accountProfile = data.accounts.find(
    (account) => account.id === session.account.id,
  );
  const displayName = accountProfile?.fullName || session.account.username;
  const [collapsed, setCollapsed] = useState(false);
  const [search, setSearch] = useState("");
  const [profileOpen, setProfileOpen] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [logoutError, setLogoutError] = useState<unknown>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    function shortcut(event: KeyboardEvent) {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        searchRef.current?.focus();
      }
    }
    window.addEventListener("keydown", shortcut);
    return () => window.removeEventListener("keydown", shortcut);
  }, []);
  const applicationCatalog = useApplicationCatalog();
  const hasRuntimePage = (route: string) =>
    applicationCatalog.data?.items.some((application) =>
      application.protocol?.pages.some((page) => page.route === route),
    ) ?? false;
  const runtimeIcons: Record<string, typeof Boxes> = {
    boxes: Boxes,
    dashboard: LayoutDashboard,
    clock: Clock,
    users: Users,
    network: Building2,
    shield: KeyRound,
  };
  const links = [
    ...(applicationCatalog.data?.items ?? [])
      .filter((application) => application.id !== "automation")
      .flatMap((application) =>
        (application.navigation ?? []).flatMap(
          (nav: { pageId: string; labelKey: string; icon?: string }) => {
            const page = (
              application.protocol?.pages ?? application.pages
            ).find(
              (page: { id: string; route: string }) => page.id === nav.pageId,
            );
            if (page?.route === "/runtime-protocol") return [];
            return page && !page.route.includes(":")
              ? [
                  {
                    to: page.route,
                    label:
                      application.messages?.find(
                        (message) => message.key === nav.labelKey,
                      )?.[i18n.language.startsWith("zh") ? "zhCN" : "en"] ??
                      t(nav.labelKey),
                    icon: runtimeIcons[nav.icon ?? "boxes"] ?? Boxes,
                    permissions:
                      application.permission === "authenticated"
                        ? []
                        : [application.permission],
                  },
                ]
              : [];
          },
        ),
      ),
    {
      to: "/applications",
      label: t("runtime:applications"),
      icon: Building2,
      permissions: ["applications:read"],
    },
    {
      to: "/ai",
      label: t("ai:nav"),
      icon: Sparkles,
      permissions: ["assistant:use"],
    },
    {
      to: "/knowledge",
      label: t("knowledge:nav"),
      icon: BookOpen,
      permissions: ["knowledge:read"],
    },
    {
      to: hasRuntimePage("/ai-settings") ? "/ai-settings" : "",
      label: t("governance:aiSettings.title"),
      icon: SlidersHorizontal,
      permissions: ["aiConfig:read"],
    },
    {
      to: hasRuntimePage("/settings") ? "/settings" : "",
      label: t("governance:settings"),
      icon: Settings2,
      permissions: ["settings:read", "dictionaries:read"],
    },
  ];
  const advancedLinks = [
    {
      to: "/capabilities",
      label: t("capability:nav"),
      icon: Boxes,
      permissions: ["example:read", "executionPlans:read"],
    },
    {
      to: "/orchestration",
      label: t("orchestration:nav"),
      icon: Workflow,
      permissions: ["orchestration:read"],
    },
  ];
  const permitted = (items: typeof links) =>
    items.filter(
      (link) =>
        link.to !== "" && canShowMenu(link.permissions, session.permissions),
    );
  const matched = search.trim().toLowerCase();
  const results = matched
    ? [
        ...data.accounts
          .filter((item) =>
            `${item.fullName} ${item.username}`.toLowerCase().includes(matched),
          )
          .map((item) => ({
            key: `a-${item.id}`,
            label: item.fullName,
            kind: t("admin:nav.accounts"),
            href: `/accounts?q=${encodeURIComponent(item.username)}`,
            icon: Users,
          })),
        ...data.organizations
          .filter((item) =>
            `${item.name} ${item.code}`.toLowerCase().includes(matched),
          )
          .map((item) => ({
            key: `o-${item.id}`,
            label: item.name,
            kind: t("admin:nav.organizations"),
            href: `/organizations?q=${encodeURIComponent(item.code)}`,
            icon: Building2,
          })),
        ...data.departments
          .filter((item) =>
            `${item.name} ${item.code}`.toLowerCase().includes(matched),
          )
          .map((item) => ({
            key: `d-${item.id}`,
            label: item.name,
            kind: t("admin:nav.departments"),
            href: `/departments?q=${encodeURIComponent(item.code)}`,
            icon: Network,
          })),
        ...data.roles
          .filter((item) =>
            `${item.name} ${item.code}`.toLowerCase().includes(matched),
          )
          .map((item) => ({
            key: `r-${item.id}`,
            label: item.name,
            kind: t("admin:nav.roles"),
            href: `/roles?q=${encodeURIComponent(item.code)}`,
            icon: ShieldCheck,
          })),
      ].slice(0, 7)
    : [];
  return (
    <div className={`admin-app ${collapsed ? "navigation-collapsed" : ""}`}>
      <a href="#admin-content" className="skip-link">
        {t("navigation:skip")}
      </a>
      <div className="console-frame">
        <aside className="console-sidebar">
          <Link to="/" className="console-brand">
            <PublishedLogo />
            <BrandMark />
            <div>
              <strong>
                {String(config.value("branding.name", t("admin:brand")))}
              </strong>
              <span>{brand.text("branding.tagline", t("admin:subtitle"))}</span>
            </div>
          </Link>
          <nav className="console-nav" aria-label={t("navigation:main")}>
            <div className="nav-section-label">{t("admin:nav.main")}</div>
            {permitted(links.slice(0, 3)).map((link) => (
              <NavLink
                key={link.to}
                to={link.to}
                onClick={() => recordNavigation(link.to)}
                end={link.to !== studioProjectsPath}
                className={({ isActive }) => (isActive ? "active" : "")}
              >
                <link.icon size={17} />
                <span>{link.label}</span>
                <ArrowUpRight size={13} />
              </NavLink>
            ))}
            <div className="nav-section-label second-label">
              {t("admin:nav.management")}
            </div>
            {permitted(links.slice(3)).map((link) => (
              <NavLink
                to={link.to}
                onClick={() => recordNavigation(link.to)}
                aria-label={link.label}
                key={link.to}
                className={({ isActive }) => (isActive ? "active" : "")}
              >
                <link.icon size={17} />
                <span>{link.label}</span>
                {link.to === "/accounts" && (
                  <small>{data.accounts.length}</small>
                )}
              </NavLink>
            ))}
          </nav>
          <div className="sidebar-bottom">
            <button
              className="sidebar-guide"
              onClick={() => setAdvancedOpen(true)}
            >
              <Settings2 size={16} />
              <span>{t("admin:nav.advanced")}</span>
              <ArrowUpRight size={13} />
            </button>
            <div className="sidebar-security">
              <ShieldCheck size={17} />
              <div>
                <strong>{t("admin:overview.security")}</strong>
                <small>{t("admin:overview.securityHint")}</small>
              </div>
            </div>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <button className="sidebar-profile">
                    <span className="person-avatar tone-1">
                      {initials(displayName)}
                    </span>
                    <span>
                      <strong>{displayName}</strong>
                    </span>
                    <ChevronDown size={13} />
                  </button>
                }
              />
              <DropdownMenuContent align="start">
                <DropdownMenuItem onClick={() => setProfileOpen(true)}>
                  <UserRound size={15} />
                  {t("admin:profile.title")}
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => navigate("/account-security")}>
                  <KeyRound size={15} />
                  {t("admin:security.title")}
                </DropdownMenuItem>
                {hasRuntimePage("/iam-governance") && (
                  <DropdownMenuItem onClick={() => navigate("/iam-governance")}>
                    <KeyRound size={15} />
                    {t("admin:policy.title")}
                  </DropdownMenuItem>
                )}
                {hasRuntimePage("/mcp") && (
                  <DropdownMenuItem onClick={() => navigate("/mcp")}>
                    <Command size={15} />
                    {t("mcp:hub.nav")}
                  </DropdownMenuItem>
                )}
                <DropdownMenuItem
                  onClick={() => {
                    setLogoutError(null);
                    void auth
                      .signOut()
                      .then(() => navigate("/login", { replace: true }))
                      .catch(setLogoutError);
                  }}
                >
                  <LogOut size={15} />
                  {t("admin:actions.signOut")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </aside>
        <div className="console-main">
          <header className="console-topbar">
            <button
              className="icon-button sidebar-toggle"
              onClick={() => setCollapsed((value) => !value)}
              aria-label={t("admin:actions.collapse")}
            >
              <PanelLeft size={17} />
            </button>
            <span className="topbar-divider" />
            <div className="global-search">
              <Search size={16} />
              <input
                ref={searchRef}
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Escape") setSearch("");
                }}
                placeholder={t("admin:search.placeholder")}
                aria-label={t("admin:search.placeholder")}
                aria-expanded={!!matched}
                aria-controls="workspace-search-results"
              />
              <kbd>
                <Command size={11} />
                {t("admin:search.shortcut")}
              </kbd>
              {matched && (
                <div
                  className="global-search-results"
                  id="workspace-search-results"
                >
                  <div>
                    {t("admin:search.results")}
                    <button
                      aria-label={t("admin:actions.close")}
                      onClick={() => setSearch("")}
                    >
                      <X size={13} />
                    </button>
                  </div>
                  {results.map((result) => (
                    <Link
                      key={result.key}
                      to={result.href}
                      onClick={() => setSearch("")}
                    >
                      <result.icon size={16} />
                      <span>
                        <strong>{result.label}</strong>
                        <small>{result.kind}</small>
                      </span>
                      <ArrowUpRight size={13} />
                    </Link>
                  ))}
                  {!results.length && <p>{t("admin:search.none")}</p>}
                </div>
              )}
            </div>
            <div className="topbar-actions">
              <NavLink
                to="/temporary-access"
                className={`${buttonVariants({ variant: "outline", size: "icon-sm" })} topbar-temporary-access`}
                aria-label={t("admin:temporary.create")}
                title={t("admin:temporary.create")}
              >
                <KeyRound aria-hidden="true" />
              </NavLink>
              <NotificationBell />
              <select
                aria-label={t("common:language")}
                value={i18n.resolvedLanguage}
                onChange={(event) => {
                  void changeLanguage(event.target.value);
                }}
              >
                <option value="en">{t("admin:locale.en")}</option>
                <option value="zh-CN">{t("admin:locale.zh")}</option>
              </select>
              <button
                className="icon-button"
                onClick={toggle}
                aria-label={t("admin:theme.toggle")}
                title={
                  theme === "dark"
                    ? t("admin:theme.light")
                    : t("admin:theme.dark")
                }
              >
                {theme === "dark" ? <Sun size={17} /> : <Moon size={17} />}
              </button>
              {hasRuntimePage("/preferences") && (
                <button
                  className="icon-button"
                  onClick={() => navigate("/preferences")}
                  aria-label={t("governance:preferences")}
                >
                  <Settings2 size={17} />
                </button>
              )}
            </div>
          </header>
          <main className="console-content" id="admin-content">
            {!!logoutError && <ErrorBanner error={logoutError} />}
            <WorkspaceHost />
          </main>
          <footer className="console-footer">
            <span>{brand.text("branding.footer", t("admin:footer"))}</span>
            <span>
              <i />
              {t("admin:overview.scopeNote")}
            </span>
          </footer>
        </div>
      </div>
      <EditorDialog
        open={profileOpen}
        onOpenChange={setProfileOpen}
        title={t("admin:profile.title")}
        description={t("admin:profile.session")}
      >
        <div className="profile-summary">
          <span className="person-avatar tone-1">{initials(displayName)}</span>
          <h3>{displayName}</h3>
          {accountProfile?.email && <p>{accountProfile.email}</p>}
          <StatusBadge status={session.account.status} />
          {session.actingOrganizationName && (
            <dl>
              <div>
                <dt>{t("admin:fields.organization")}</dt>
                <dd>{session.actingOrganizationName}</dd>
              </div>
            </dl>
          )}
        </div>
      </EditorDialog>
      <EditorDialog
        open={advancedOpen}
        onOpenChange={setAdvancedOpen}
        title={t("admin:nav.advanced")}
        description={t("admin:nav.advancedDescription")}
      >
        <div className="access-guide">
          {permitted(advancedLinks).map((link) => (
            <div key={link.to}>
              <link.icon size={17} />
              <Link to={link.to} onClick={() => setAdvancedOpen(false)}>
                {link.label}
              </Link>
            </div>
          ))}
        </div>
      </EditorDialog>
    </div>
  );
}
