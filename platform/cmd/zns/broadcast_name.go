package main

import (
	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

func broadcastOptions(model agent.Model) runtimeapp.Options {
	if namer, ok := model.(agent.BroadcastNamer); ok {
		return runtimeapp.Options{InformalName: namer.InformalName}
	}
	return runtimeapp.Options{}
}
