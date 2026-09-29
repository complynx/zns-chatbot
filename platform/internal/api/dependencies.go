package api

import (
	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/adminutilities"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/media"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

// Dependencies contains ready services. HTTP adapts requests but does not
// construct domain services or start their background work.
type Dependencies struct {
	Core           core.Service
	Orders         orders.Service
	LegacyOrders   orders.Service
	Conversation   conversation.Service
	Knowledge      knowledge.Service
	Media          media.Service
	Massage        massage.Service
	PassProfiles   passes.Service
	Registration   passbooking.Service
	LegacyFood     legacyfood.Service
	AdminMessages  adminmessage.Service
	AdminUtilities adminutilities.Service
	ModelSettings  modelsettings.Service
	Credits        credits.Service
}
