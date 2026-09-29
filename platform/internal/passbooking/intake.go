package passbooking

import "context"

// RegistrationIntakeResolver validates earlier classified native requests before
// allocation. Identity providers are called before any domain transaction opens.
type RegistrationIntakeResolver interface {
	ResolveRegistrationIntake(context.Context, string) error
}

func (s Service) ResolveRegistrationIntake(ctx context.Context, event string) error {
	if s.Intake == nil {
		return nil
	}
	return s.Intake.ResolveRegistrationIntake(ctx, event)
}
