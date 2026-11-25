package identityapp

import (
	"context"

	genidentity "horizon/foundation/gen/identity"
	iam "horizon/foundation/system/identity"
)

type Authentication struct {
	IAM *iam.Service
}

func (s Authentication) Login(ctx context.Context, payload *genidentity.LoginPayload) (*genidentity.LoginResult, error) {
	return s.IAM.AuthenticatePassword(ctx, payload)
}

func (s Authentication) RequestRegistration(ctx context.Context, payload *genidentity.RequestRegistrationPayload) error {
	return s.IAM.BeginApplicationRegistration(ctx, payload)
}

func (s Authentication) CompleteRegistration(ctx context.Context, payload *genidentity.CompleteRegistrationPayload) error {
	return s.IAM.VerifyApplicationRegistration(ctx, payload)
}

func (s Authentication) RequestPasswordReset(ctx context.Context, payload *genidentity.RequestPasswordResetPayload) error {
	return s.IAM.BeginApplicationPasswordRecovery(ctx, payload)
}

func (s Authentication) CompletePasswordReset(ctx context.Context, payload *genidentity.CompletePasswordResetPayload) error {
	return s.IAM.VerifyApplicationPasswordRecovery(ctx, payload)
}
