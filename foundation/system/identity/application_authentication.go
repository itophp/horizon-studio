package identity

import "context"

type applicationAuthenticationKey struct{}

// WithApplicationAuthentication binds a trusted application credential
// namespace resolved by the transport adapter.
func WithApplicationAuthentication(ctx context.Context, namespace string) context.Context {
	return context.WithValue(ctx, applicationAuthenticationKey{}, namespace)
}

func applicationAuthentication(ctx context.Context) (string, error) {
	namespace, _ := ctx.Value(applicationAuthenticationKey{}).(string)
	if !loginRealmPattern.MatchString(namespace) {
		return "", problem("forbidden")
	}
	return namespace, nil
}
