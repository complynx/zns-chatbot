package agenthost

import (
	"github.com/complynx/zns-chatbot/platform/internal/account"
)

func BindScriptToolKey(call *ScriptToolRecord, key string, foodSequence int) {
	if call.Food != nil {
		call.Food.Key = key
		call.FoodSequence = foodSequence
	}
	if call.Profile != nil {
		call.Profile.Key = key
	}
	if call.Language != nil {
		call.Language.Key = account.LanguageOperationKey(key)
	}
	if call.Model != nil && call.Model.Grant != nil {
		call.Model.Grant.OperationKey = key
	}
	if call.Model != nil && call.Model.Change != nil {
		call.Model.Change.OperationKey = key
	}
	if call.Massage != nil && call.Massage.Command != nil {
		call.Massage.Command.Key = key
	}
	if call.Memory != nil {
		call.Memory.Key = key
	}
	if call.Order != nil {
		call.Order.Key = key
		call.Order.CatalogSnapshot = call.ChoiceCatalog
		if call.Source != nil && (call.Order.Name == "create" || call.Order.Name == "edit") {
			generation := *call.Source.Generation
			call.Order.HistoryGeneration = &generation
		}
	}
	if call.Action != nil {
		call.Action.Key = key
	}
	bindRegistrationToolKey(call.Pass, key)
}

func bindRegistrationToolKey(request *ScriptPassRequest, key string) {
	if request == nil {
		return
	}
	if request.Command != nil && request.Command.Key == "" {
		request.Command.Key = key
	}
	if request.Assignment != nil && request.Assignment.Key == "" {
		request.Assignment.Key = key
	}
	if request.Batch != nil && request.Batch.Key == "" {
		request.Batch.Key = key
	}
}
