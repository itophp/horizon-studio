import {
  Fragment,
  createContext,
  useContext,
  useEffect,
  type PropsWithChildren,
} from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  identityCompleteFederation,
  identityCompleteSaml,
  identityLogout,
  identityRefreshSession,
  identitySelectSignInIdentity,
  identityMe,
  getIdentityMeQueryKey,
  type identityMeResponse,
} from "@/api/generated/baseline";
import type {
  CompleteFederationRequestBody,
  CompleteSamlRequestBody,
  LoginRequestBody,
  RefreshSessionRequestBody,
  SessionProfile,
} from "@/api/generated/models";
import { ApiError, setCSRFToken } from "@/api/client/request";
import { applicationLogin } from "./application-authentication";
type AuthenticatedProfile = SessionProfile & {
  account: NonNullable<SessionProfile["account"]>;
};

interface AuthValue {
  profile: AuthenticatedProfile | undefined;
  loginOnly?: boolean;
  loading: boolean;
  error: unknown;
  signIn: (data: LoginRequestBody, application?: string) => Promise<void>;
  finishFederation: (
    data: CompleteFederationRequestBody,
  ) => Promise<string | undefined>;
  finishSaml: (data: CompleteSamlRequestBody) => Promise<string | undefined>;
  signOut: () => Promise<void>;
  selectSignInIdentity: (identityBindingId?: string) => Promise<SessionProfile>;
  refresh: () => Promise<void>;
  renewSession?: (
    input: RefreshSessionRequestBody,
    signal: AbortSignal,
  ) => Promise<void>;
}
const AuthContext = createContext<AuthValue | null>(null);
export function AuthProvider({ children }: PropsWithChildren) {
  const client = useQueryClient();
  // The token is stored here, before the profile is published: every capability
  // call is a POST and needs the nonce, and a reload must not race it.
  const query = useQuery({
    queryKey: getIdentityMeQueryKey(),
    queryFn: async () => {
      const response = await identityMe();
      if (response.status === 200) {
        const previous = client.getQueryData<identityMeResponse>(
          getIdentityMeQueryKey(),
        );
        if (
          previous?.status === 200 &&
          previous.data.authorizationVersion &&
          previous.data.authorizationVersion !==
            response.data.authorizationVersion
        ) {
          await client.cancelQueries({
            predicate: (item) =>
              item.queryKey[0] !== getIdentityMeQueryKey()[0],
          });
          client.removeQueries({
            predicate: (item) =>
              item.queryKey[0] !== getIdentityMeQueryKey()[0],
          });
        }
        setCSRFToken(response.data.csrfToken);
      }
      return response as identityMeResponse;
    },
    retry: false,
    staleTime: 60000,
    refetchOnWindowFocus: true,
    refetchInterval: 30000,
  });
  const session =
    query.data?.status === 200 &&
    !(query.error instanceof ApiError && query.error.status === 401)
      ? query.data.data
      : undefined;
  const profile = session?.account
    ? { ...session, account: session.account }
    : undefined;
  const loginOnly = !!session && !session.account;
  useEffect(() => {
    setCSRFToken(session?.csrfToken ?? "");
    void client.invalidateQueries({
      predicate: (item) => item.queryKey[0] !== getIdentityMeQueryKey()[0],
    });
  }, [session?.csrfToken, session?.authorizationVersion, client]);
  useEffect(() => {
    const refresh = () => {
      void client.invalidateQueries({ queryKey: getIdentityMeQueryKey() });
    };
    window.addEventListener("baseline:unauthenticated", refresh);
    return () =>
      window.removeEventListener("baseline:unauthenticated", refresh);
  }, [client]);
  const clearPrivateQueries = () =>
    client.removeQueries({
      predicate: (item) => item.queryKey[0] !== getIdentityMeQueryKey()[0],
    });
  const signIn = async (data: LoginRequestBody, application?: string) => {
    const response = await applicationLogin(application, data);
    if (response.status !== 200)
      throw new ApiError("unauthenticated", response.status);
    await publishSession(response.data.profile, response.headers);
  };
  const publishSession = async (next: SessionProfile, headers: Headers) => {
    window.dispatchEvent(new Event("baseline:webmcp-revoke"));
    await client.cancelQueries();
    clearPrivateQueries();
    setCSRFToken(next.csrfToken);
    client.setQueryData<identityMeResponse>(getIdentityMeQueryKey(), {
      data: next,
      status: 200,
      headers,
    });
  };
  const signOut = async () => {
    window.dispatchEvent(new Event("baseline:webmcp-revoke"));
    await identityLogout();
    clearPrivateQueries();
    setCSRFToken("");
    client.setQueryData<identityMeResponse>(getIdentityMeQueryKey(), {
      data: { code: "unauthenticated" },
      status: 401,
      headers: new Headers(),
    });
  };
  return (
    <AuthContext.Provider
      value={{
        profile,
        loginOnly,
        loading: query.isPending,
        error: query.error,
        signIn,
        finishFederation: async (data) => {
          const response = await identityCompleteFederation(data);
          if (response.status !== 200)
            throw new ApiError("unauthenticated", response.status);
          if (response.data.profile)
            await publishSession(response.data.profile, response.headers);
          else if (!response.data.linkProof)
            throw new ApiError("unauthenticated", 401);
          return response.data.linkProof;
        },
        finishSaml: async (data) => {
          const response = await identityCompleteSaml(data);
          if (response.status !== 200)
            throw new ApiError("unauthenticated", response.status);
          if (response.data.profile)
            await publishSession(response.data.profile, response.headers);
          else if (!response.data.linkProof)
            throw new ApiError("unauthenticated", 401);
          return response.data.linkProof;
        },
        signOut,
        renewSession: async (input, signal) => {
          const response = await identityRefreshSession(input, { signal });
          if (response.status !== 200)
            throw new ApiError("forbidden", response.status);
          if (!signal.aborted)
            await publishSession(response.data.profile, response.headers);
        },
        selectSignInIdentity: async (identityBindingId) => {
          window.dispatchEvent(new Event("baseline:webmcp-revoke"));
          await client.cancelQueries();
          const response = await identitySelectSignInIdentity({
            identityBindingId,
          });
          if (response.status !== 200)
            throw new ApiError("forbidden", response.status);
          await publishSession(response.data, response.headers);
          return response.data;
        },
        refresh: async () => {
          await client.invalidateQueries({ queryKey: getIdentityMeQueryKey() });
        },
      }}
    >
      <Fragment
        key={JSON.stringify([
          profile?.tenantId,
          profile?.account.id,
          profile?.identityBindingId,
          profile?.authorizationVersion,
          loginOnly,
        ])}
      >
        {children}
      </Fragment>
    </AuthContext.Provider>
  );
}
export function useAuth() {
  const value = useContext(AuthContext);
  if (!value) throw new Error("AuthProvider is required");
  return value;
}
export function useSession() {
  const { profile } = useAuth();
  if (!profile) throw new Error("A verified session is required");
  return profile;
}
export function usePermission(permission: string) {
  return useSession().permissions.includes(permission);
}
