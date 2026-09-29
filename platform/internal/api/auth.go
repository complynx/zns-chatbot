package api

import "context"

// VerifyOwner resolves an authenticated token to the existing business owner.
type VerifyOwner func(context.Context, string) (string, error)

type TokenVerifier interface {
	Verify(context.Context, string) (string, error)
}

type SubjectLinks interface {
	Subject(context.Context, string) (string, error)
}

func ZitadelOwner(verifier TokenVerifier, links SubjectLinks) VerifyOwner {
	return func(ctx context.Context, token string) (string, error) {
		subject, err := verifier.Verify(ctx, token)
		if err != nil {
			return "", err
		}
		return links.Subject(ctx, subject)
	}
}
