export function sessionLandingPath(
  portal: string | undefined,
  fallback: string,
) {
  if (portal === "supplier-portal") return "/account-security";
  return portal ? `/${encodeURIComponent(portal)}` : fallback;
}
