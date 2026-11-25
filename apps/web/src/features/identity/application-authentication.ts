import { request } from "@/api/client/request";
import {
  getIdentityCompletePasswordResetUrl,
  getIdentityCompleteRegistrationUrl,
  getIdentityLoginUrl,
  getIdentityRequestPasswordResetUrl,
  getIdentityRequestRegistrationUrl,
  identityCompletePasswordReset,
  identityCompleteRegistration,
  identityLogin,
  identityRequestPasswordReset,
  identityRequestRegistration,
} from "@/api/generated/baseline";

function applicationRequest<Input, Output>(
  execute: (input: Input, options?: RequestInit) => Promise<Output>,
  url: () => string,
) {
  return (
    application: string | undefined,
    input: Input,
    options?: RequestInit,
  ) => {
    if (!application) return execute(input, options);
    const action = url().slice("/api/auth/".length);
    return request<Output>(
      "/api/auth/applications/" +
        encodeURIComponent(application) +
        "/" +
        action,
      {
        ...options,
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(input),
      },
    );
  };
}

export const applicationLogin = applicationRequest(
  identityLogin,
  getIdentityLoginUrl,
);
export const applicationRequestRegistration = applicationRequest(
  identityRequestRegistration,
  getIdentityRequestRegistrationUrl,
);
export const applicationCompleteRegistration = applicationRequest(
  identityCompleteRegistration,
  getIdentityCompleteRegistrationUrl,
);
export const applicationRequestPasswordReset = applicationRequest(
  identityRequestPasswordReset,
  getIdentityRequestPasswordResetUrl,
);
export const applicationCompletePasswordReset = applicationRequest(
  identityCompletePasswordReset,
  getIdentityCompletePasswordResetUrl,
);
