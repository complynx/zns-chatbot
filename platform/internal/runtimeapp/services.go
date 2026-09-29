// Package runtimeapp constructs application dependencies without starting work.
package runtimeapp

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/adminutilities"
	"github.com/complynx/zns-chatbot/platform/internal/api"
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

type Options struct {
	LegacyOrderBotID int64
	InformalName     adminmessage.InformalName
}

// NewServices only assembles values. Callers retain migration, seed and lifecycle
// ownership; tests and product runtime use the same construction path.
func NewServices(db *pgxpool.Pool, options Options) api.Dependencies {
	return api.Dependencies{
		Core:         core.Service{DB: db, LegacyOrderBotID: options.LegacyOrderBotID},
		Orders:       orders.Service{DB: db},
		LegacyOrders: orders.Service{DB: db, LegacyBotID: options.LegacyOrderBotID},
		Conversation: conversation.Service{DB: db},
		Knowledge:    knowledge.Service{DB: db},
		Media:        media.Service{DB: db},
		Massage:      massage.Service{DB: db},
		PassProfiles: passes.Service{DB: db},
		Registration: passbooking.Service{DB: db},
		LegacyFood:   legacyfood.Service{DB: db, BotID: options.LegacyOrderBotID},
		AdminMessages: adminmessage.Service{
			DB:           db,
			BotID:        options.LegacyOrderBotID,
			InformalName: options.InformalName,
		},
		AdminUtilities: adminutilities.Service{DB: db},
		ModelSettings:  modelsettings.Service{DB: db},
		Credits:        credits.Service{DB: db},
	}
}
