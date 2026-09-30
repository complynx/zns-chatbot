// Package appservices constructs domain services without starting work.
package appservices

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/workflow"

	"github.com/complynx/zns-chatbot/platform/internal/account"
	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/adminutilities"
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/destination"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/media"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

// Services contains ready domain services. Transports adapt requests without
// reconstructing services or starting background work.
type Services struct {
	MemoryReadState  agenthost.MemoryReadStore
	BotDelivery      botdelivery.Service
	Workflow         workflow.Service
	Account          account.Service
	DerivedMutations derivedmutation.Service
	Core             core.Service
	Orders           orders.Service
	LegacyOrders     orders.Service
	Conversation     conversation.Service
	Knowledge        knowledge.Service
	Media            media.Service
	Massage          massage.Service
	PassProfiles     passes.Service
	Registration     passbooking.Service
	LegacyFood       legacyfood.Service
	AdminMessages    adminmessage.Service
	AdminUtilities   adminutilities.Service
	ModelSettings    modelsettings.Service
	Credits          credits.Service
}

type Options struct {
	NativeRegistrationAuthorizer derivedmutation.NativeRegistrationAuthorizer
	RegistrationRetention        time.Duration
	LegacyOrderBotID             int64
	InformalName                 adminmessage.InformalName
	Delivery                     delivery.Settings
	AnnouncementBindings         *destination.Bindings
	DestinationResolver          destination.Resolver
}

// NewServices only assembles values. Callers retain migration, seed and lifecycle
// ownership; tests and product runtime use the same construction path.
func NewServices(db *pgxpool.Pool, options Options) Services {
	accountService := account.Service{DB: db}
	workflowService := workflow.Service{DB: db}
	coreService := core.Service{DB: db, LegacyOrderBotID: options.LegacyOrderBotID}
	orderService := orders.Service{DB: db, Delivery: options.Delivery}
	registration := passbooking.Service{
		DB: db, Delivery: options.Delivery, RegistrationRetention: options.RegistrationRetention,
		AnnouncementBindings: options.AnnouncementBindings,
	}
	registration.Intake = &derivedmutation.NativeRegistrationResolver{
		Service: derivedmutation.Service{
			DB:           db,
			Registration: registration,
		},
		Authorize: options.NativeRegistrationAuthorizer,
	}
	profile := passes.Service{DB: db}
	food := legacyfood.Service{DB: db, BotID: options.LegacyOrderBotID, Delivery: options.Delivery}
	massageService := massage.Service{DB: db, Delivery: options.Delivery}
	settings := modelsettings.Service{DB: db}
	creditService := credits.Service{DB: db}
	return Services{
		MemoryReadState: agenthost.MemoryReadStore{DB: db},
		BotDelivery: botdelivery.Service{
			DB:            db,
			Delivery:      options.Delivery,
			AdminMessages: adminmessage.Service{DB: db},
			Food:          food,
		},
		DerivedMutations: derivedmutation.Service{
			DB: db, Orders: orderService, Workflow: workflowService, Account: accountService,
			Registration: registration, Profile: profile, Food: food,
			Massage: massageService, ModelSettings: settings, Credits: creditService,
		},
		Core:         coreService,
		Workflow:     workflowService,
		Account:      accountService,
		Orders:       orderService,
		LegacyOrders: orders.Service{DB: db, LegacyBotID: options.LegacyOrderBotID, Delivery: options.Delivery},
		Conversation: conversation.Service{DB: db},
		Knowledge:    knowledge.Service{DB: db},
		Media:        media.Service{DB: db},
		Massage:      massageService,
		PassProfiles: profile,
		Registration: registration,
		LegacyFood:   food,
		AdminMessages: adminmessage.Service{
			DB:                  db,
			BotID:               options.LegacyOrderBotID,
			InformalName:        options.InformalName,
			Delivery:            options.Delivery,
			DestinationResolver: options.DestinationResolver,
		},
		AdminUtilities: adminutilities.Service{DB: db, Registration: registration},
		ModelSettings:  settings,
		Credits:        creditService,
	}
}
