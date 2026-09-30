package agenthost

import (
	"context"
	"encoding/json"

	"github.com/complynx/zns-chatbot/platform/internal/account"
	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/workflow"
)

type ScriptEvaluator interface {
	Evaluate(context.Context, scriptclient.Request) (json.RawMessage, error)
}

type ScriptRecord struct {
	PrivateHistory    bool                                `json:"private_history"`
	ReadAuthorities   []readsource.Authority              `json:"read_authorities"`
	PassContext       []interaction.PassContextDependency `json:"pass_context"`
	HistoryGeneration int64                               `json:"history_generation"`
	HistoryRedacted   bool                                `json:"history_redacted,omitempty"`
	PassRedacted      bool                                `json:"pass_redacted,omitempty"`
	PrivateProfile    bool                                `json:"private_profile,omitempty"`
	MemoryRedacted    bool                                `json:"memory_redacted,omitempty"`
	MemoryState       knowledge.MemoryDeletionState       `json:"memory_state"`
	Calls             []ScriptToolRecord                  `json:"calls,omitempty"`
	Request           agent.ScriptProposal                `json:"request"`
	Run               agent.ScriptRun                     `json:"run"`
}

type ScriptToolRecord struct {
	KnowledgeRefreshPending bool                              `json:"knowledge_refresh_pending,omitempty"`
	PrivilegedRead          *ScriptPrivilegedRead             `json:"privileged_read,omitempty"`
	ResultAuthorities       []readsource.Authority            `json:"result_authorities"`
	Source                  *readsource.Derivation            `json:"source,omitempty"`
	MemoryReadState         *knowledge.MemoryDeletionState    `json:"memory_read_state,omitempty"`
	KnowledgeRead           *knowledgeauthority.ReadAuthority `json:"knowledge_read,omitempty"`
	ChoiceGeneration        *int64                            `json:"choice_generation,omitempty"`
	ChoiceCatalog           string                            `json:"choice_catalog,omitempty"`
	ModernChoice            *ModernChoiceRecord               `json:"modern_choice,omitempty"`
	ChoiceUse               string                            `json:"choice_use,omitempty"`
	ModernOrder             *ModernOrderRequest               `json:"modern_order,omitempty"`
	CreditPolicy            *CreditToolCommand                `json:"credit_policy,omitempty"`
	PassReceiptID           string                            `json:"pass_receipt_id,omitempty"`
	Pass                    *ScriptPassRequest                `json:"pass,omitempty"`
	BroadcastReview         *BroadcastReviewRequest           `json:"broadcast_review,omitempty"`
	FoodExport              *FoodExportRequest                `json:"food_export,omitempty"`
	FoodSequence            int                               `json:"food_sequence,omitempty"`
	Food                    *legacyfood.Command               `json:"food,omitempty"`
	Model                   *ScriptModelRequest               `json:"model,omitempty"`
	Massage                 *ScriptMassageRequest             `json:"massage,omitempty"`
	Broadcast               *BroadcastToolRequest             `json:"broadcast,omitempty"`
	Memory                  *knowledge.Command                `json:"memory,omitempty"`
	Order                   *orders.Command                   `json:"order,omitempty"`
	Action                  *workflow.Action                  `json:"action,omitempty"`
	Outcome                 agent.ScriptToolResult            `json:"outcome"`
	Profile                 *ScriptProfileMutation            `json:"profile,omitempty"`
	Language                *ScriptLanguageMutation           `json:"language,omitempty"`
	PassRead                *ScriptDomainEventArguments       `json:"pass_read,omitempty"`
}

type ScriptToolEntry struct {
	Descriptor  scriptclient.Tool
	Prepare     func(context.Context, string, int64, scriptclient.ToolCall, agent.Input) (ScriptToolRecord, error)
	Execute     func(context.Context, string, scriptclient.ToolCall, ScriptToolRecord, *agent.Input) (any, error)
	ResultLimit int
}

type ScriptPrivilegedRead struct {
	Event     string                 `json:"event,omitempty"`
	Owner     string                 `json:"owner"`
	Admission []readsource.Authority `json:"admission,omitempty"`
}

type ModernChoiceRecord struct {
	HistoryGeneration *int64        `json:"history_generation,omitempty"`
	Ref               string        `json:"ref"`
	Parent            string        `json:"parent,omitempty"`
	Child             string        `json:"child,omitempty"`
	Depth             int           `json:"depth"`
	Event             string        `json:"event"`
	Catalog           string        `json:"catalog"`
	OrderID           string        `json:"order_id,omitempty"`
	Snapshot          string        `json:"snapshot,omitempty"`
	Choice            orders.Choice `json:"choice"`
}

type ModernOrderRequest struct {
	ReadOrderID  string `json:"read_order_id,omitempty"`
	ReadCursor   string `json:"read_cursor,omitempty"`
	ReadSnapshot string `json:"read_snapshot,omitempty"`
	Event        string `json:"event"`
	Update       int64  `json:"update"`
	Chat         int64  `json:"chat,omitempty"`
}

type CreditToolCommand struct {
	Payer  string               `json:"payer"`
	Change credits.PolicyChange `json:"change"`
}

type ScriptPassRequest struct {
	Witness      *passbooking.OperationWitness `json:"witness,omitempty"`
	Menu         *interaction.RegistrationMenu `json:"menu,omitempty"`
	ID           string                        `json:"id"`
	Name         string                        `json:"name"`
	Command      *passbooking.Command          `json:"command,omitempty"`
	Assignment   *passbooking.AdminAssignment  `json:"assignment,omitempty"`
	Batch        *passbooking.RuntimeBatch     `json:"batch,omitempty"`
	ExportUpdate int64                         `json:"export_update,omitempty"`
	Chat         int64                         `json:"chat,omitempty"`
}

type BroadcastReviewArguments struct {
	ID     int64  `json:"id"`
	Offset int64  `json:"offset,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type BroadcastReviewRequest struct {
	Owner     string                   `json:"owner"`
	Chat      int64                    `json:"chat"`
	Arguments BroadcastReviewArguments `json:"arguments"`
}

type FoodExportRequest struct {
	Source   *readsource.Derivation `json:"source,omitempty"`
	ID       string                 `json:"id"`
	EventID  string                 `json:"event_id"`
	UpdateID int64                  `json:"update_id"`
	ChatID   int64                  `json:"chat_id"`
}

type ScriptModelRequest struct {
	Endpoint string                `json:"endpoint"`
	Scope    string                `json:"scope,omitempty"`
	Change   *modelsettings.Change `json:"change,omitempty"`
	Grant    *modelsettings.Grant  `json:"grant,omitempty"`
}

type ScriptMassageRequest struct {
	Command     *massage.Command     `json:"command,omitempty"`
	Preferences *massage.Preferences `json:"preferences,omitempty"`
	Event       string               `json:"event"`
	Update      int64                `json:"update"`
}

type BroadcastToolRequest struct {
	Command    string                   `json:"command,omitempty"`
	InputID    int64                    `json:"input_id,omitempty"`
	Key        string                   `json:"key"`
	Attachment *adminmessage.Attachment `json:"attachment,omitempty"`
}

type ScriptProfileMutation struct {
	Field   string `json:"field"`
	Version int64  `json:"version"`
	Key     string `json:"key"`
	Value   string `json:"-"`
}

type ScriptLanguageMutation struct {
	Language string                       `json:"language"`
	Key      account.LanguageOperationKey `json:"key"`
}

type ScriptDomainEventArguments struct {
	Event  string `json:"event"`
	Cursor string `json:"cursor,omitempty"`
}
