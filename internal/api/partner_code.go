package api

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

const partnerMetadataTimeout = 2 * time.Second

type partnerCodeResult struct {
	PartnerCode uuid.UUID `json:"partner_code"`
	ShareURL    string    `json:"share_url,omitempty"`
}

func (s *Server) registerPartnerCode(api huma.API) {
	registerAgentPost(
		api,
		"agent-own-partner-code",
		pairInvitationPath+"partner_code",
		internalBodyLimit,
		s.ownPartnerCode,
	)
}

func (s *Server) ownPartnerCode(
	ctx context.Context,
	in *accountRequest,
) (*agentReadResponse[partnerCodeResult], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	if len(in.Body.Arguments) != 0 {
		return nil, huma.Error400BadRequest("partner_code takes no arguments")
	}
	result := partnerCodeResult{PartnerCode: scope.UserID}
	if s.options.Telegram != nil {
		lookup, cancel := context.WithTimeout(ctx, partnerMetadataTimeout)
		bot, lookupErr := s.options.Telegram.GetMe(lookup)
		cancel()
		if lookupErr == nil {
			result.ShareURL = telegram.PartnerShareURL(bot, s.options.BotID, scope.UserID)
		}
	}
	// Recheck current binding after Telegram IO, with no connection held during it.
	if _, err = s.store.GetProfile(ctx, scope); err != nil {
		return nil, profileError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func partnerCodeMethod() map[string]any {
	method := paymentMethod(
		"partner_code",
		"Read only your own public account code and optional Telegram share_url. Give this link/code to a partner so they can explicitly invite YOU after choosing an event and understanding the allocation hold; you must subsequently explicitly accept. This is contact addressing, not an already-created invitation, authentication, evidence that its owner shared it, or consent. Opening alone must not register, invite, accept, pin an event, change a profile or money. No target lookup or directory exists. If share_url is absent, Telegram bot metadata was unavailable/unverified; the plain partner_code remains usable. Raw own UUID sharing still works.",
		map[string]any{},
		nil,
		[]string{"partner_code"},
	)
	method[manifestPath] = pairInvitationPath + "partner_code"
	return method
}
