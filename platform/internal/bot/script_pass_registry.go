package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
)

const registrationOperationID = "operation_id"
const registrationAdminsView = "admins"
const scriptPassShow = "passes.registration.show"
const scriptPassRead = "passes.registration.read"
const scriptPassAdminRead = "passes.admin.queue"
const scriptPassAdminTarget = "passes.admin.target"
const scriptPassReviewRead = "passes.payments.review"
const scriptPassTakeoverRead = "passes.takeover.read"
const scriptPassTiers = "passes.tiers"
const scriptPassExport = "passes.export"
const scriptPassResume = "passes.resume"
const scriptPassOperations = "passes.operations"
const scriptPassBatchAssign = "passes.batch.assign"
const scriptPassBatchCancel = "passes.batch.cancel"
const scriptRegistrationBatchUncouple = "passes.batch.uncouple"

func (b *Bot) scriptPassEntries(ctx context.Context, owner string) ([]agenthost.ScriptToolEntry, error) {
	return (agenthost.RegistrationScriptCatalog{Client: b.API, Binding: agenthost.ScriptToolEntry{
		Prepare: b.preparePassTool, Execute: b.executePassTool,
	}}).Entries(ctx, owner)
}
