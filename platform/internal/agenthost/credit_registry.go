package agenthost

import (
	"context"
	"encoding/json"

	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptCreditUsage = "credits.usage"
const scriptCreditHistory = "credits.history"
const scriptCreditAdminUsage = "credits.admin.usage"
const scriptCreditAdminHistory = "credits.admin.history"
const scriptCreditDefault = "credits.admin.default"
const scriptCreditPolicy = "credits.admin.policy"

// CreditScriptCatalog owns live permission-based visibility and schemas.
// ReadPermissions adapts the existing domain permission contract.
type CreditScriptCatalog struct {
	ReadPermissions func(context.Context, string) (map[string]bool, error)
	Binding         ScriptToolEntry
}

func (c CreditScriptCatalog) Entries(ctx context.Context, owner string) ([]ScriptToolEntry, error) {
	permissions, err := c.ReadPermissions(ctx, owner)
	if err != nil {
		return nil, err
	}
	empty := json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	page := json.RawMessage(
		`{"type":"object","properties":{"cursor":{"type":"string","maxLength":2048}},"additionalProperties":false}`,
	)
	descriptors := []scriptclient.Tool{
		{
			Name:        scriptCreditUsage,
			Description: "Read your current monthly API credit allowance, spend, holds and unknown count. Monetary values are exact decimal strings. Unlimited usage is still tracked.",
			InputSchema: empty,
		},
		{
			Name:        scriptCreditHistory,
			Description: "Read your paid attempts, including failures and unknown costs. Follow next_cursor while more is true. Amounts are decimal strings; null is unknown, never zero.",
			InputSchema: page,
		},
	}
	if permissions["admin"] {
		descriptors = append(
			descriptors,
			scriptclient.Tool{
				Name:        scriptCreditAdminUsage,
				Description: "Read an explicit payer's credit report under your current global administrator role.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"payer":{"type":"string","maxLength":256}},"required":["payer"],"additionalProperties":false}`,
				),
			},
			scriptclient.Tool{
				Name:        scriptCreditAdminHistory,
				Description: "Read an explicit payer's paginated paid-attempt history. Current administrator access is checked on every call.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"payer":{"type":"string","maxLength":256},"cursor":{"type":"string","maxLength":2048}},"required":["payer"],"additionalProperties":false}`,
				),
			},
			scriptclient.Tool{
				Name:        scriptCreditDefault,
				Description: "Read the current ordinary monthly default and its version. No allowance carries over.",
				InputSchema: empty,
			},
			scriptclient.Tool{
				Name:        scriptCreditPolicy,
				Description: "Apply an explicitly requested administrator credit policy. Supply the observed version and exact decimal amount, unlimited or default. Payer * changes the ordinary default and requires a decimal amount. This changes future admissions, not past charges.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"payer":{"type":"string","maxLength":256},"amount":{"type":"string","maxLength":30},"version":{"type":"integer","minimum":1,"maximum":9007199254740991}},"required":["payer","amount","version"],"additionalProperties":false}`,
				),
			},
		)
	}
	entries := make([]ScriptToolEntry, 0, len(descriptors))
	for _, descriptor := range descriptors {
		entries = append(
			entries,
			ScriptToolEntry{
				Descriptor:  descriptor,
				Prepare:     c.Binding.Prepare,
				Execute:     c.Binding.Execute,
				ResultLimit: c.Binding.ResultLimit,
			},
		)
	}
	return entries, nil
}
