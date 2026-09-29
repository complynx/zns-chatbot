package bot

import (
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

const creditReportVersionField = "version"
const creditReportStateField = "state"

// Monetary strings retain all nine fractional digits across the JavaScript boundary.
func creditToolPolicy(policy credits.Policy) map[string]any {
	return map[string]any{
		"payer":                  policy.Payer,
		"monthly_credits":        creditAmount(policy.MonthlyNanoUSD),
		"unlimited":              policy.Unlimited,
		"implicit_unlimited":     policy.ImplicitUnlimited,
		"inherited":              policy.Inherited,
		creditReportVersionField: policy.Version,
	}
}
func optionalCreditAmount(value *int64) *string {
	if value == nil {
		return nil
	}
	formatted := creditAmount(*value)
	return &formatted
}
func creditToolUsage(report credits.UsageReport) map[string]any {
	return map[string]any{
		"policy":            creditToolPolicy(report.Policy),
		"period_start":      report.PeriodStart,
		"period_end":        report.PeriodEnd,
		"spent_credits":     creditAmount(report.SpentNanoUSD),
		"held_credits":      creditAmount(report.HeldNanoUSD),
		"available_credits": optionalCreditAmount(report.AvailableNanoUSD),
		"unbounded_unknown": report.UnboundedUnknown,
	}
}
func creditToolHistory(page core.ReadPage[credits.AttemptReport]) core.ReadPage[map[string]any] {
	result := core.ReadPage[map[string]any]{Items: []map[string]any{}, More: page.More, NextCursor: page.NextCursor}
	for _, item := range page.Items {
		result.Items = append(
			result.Items,
			map[string]any{
				"id":                   item.ID,
				"operation":            item.Operation,
				"provider":             item.Provider,
				"model":                item.Model,
				creditReportStateField: item.State,
				"period_start":         item.PeriodStart,
				"created_at":           item.CreatedAt,
				"cost_credits":         optionalCreditAmount(item.CostNanoUSD),
				"reconciled_credits":   optionalCreditAmount(item.ReconciledNanoUSD),
				"reserved_credits":     optionalCreditAmount(item.ReservedNanoUSD),
				"cost_basis":           item.CostBasis,
			},
		)
	}
	return result
}
