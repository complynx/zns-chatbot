package main

import (
	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
)

func broadcastOptions(model agent.Model) appservices.Options {
	if namer, ok := model.(agent.BroadcastNamer); ok {
		return appservices.Options{InformalName: namer.InformalName}
	}
	return appservices.Options{}
}
