package credits

import (
	"context"
)

func (s Service) Permissions(ctx context.Context, actor string) (map[string]bool, error) {
	allowed, err := superadmin(ctx, s.DB, actor)
	return map[string]bool{"admin": allowed}, err
}
func (s Service) DefaultPolicy(ctx context.Context, actor string) (Policy, error) {
	if err := requireAdmin(ctx, s.DB, actor); err != nil {
		return Policy{}, err
	}
	result := Policy{Payer: "*"}
	err := s.DB.QueryRow(ctx, `SELECT monthly_nano_usd,version FROM credits.default_policy WHERE singleton`).
		Scan(&result.MonthlyNanoUSD, &result.Version)
	return result, err
}
