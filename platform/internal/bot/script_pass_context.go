package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/interaction"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

// Keep authority identity, never another copy of private booking content.

func (b *Bot) passContextChanged(
	ctx context.Context,
	owner string,
	dependency interaction.PassContextDependency,
) (bool, error) {
	if agenthost.PassQueueView(dependency.Request.View) && dependency.QueueAuthorities == nil {
		return true, nil
	}
	if dependency.TargetBooking != nil || agenthost.PassQueueView(dependency.Request.View) {
		authorities, err := agenthost.PassContextReadAuthorities([]interaction.PassContextDependency{dependency})
		if err != nil {
			return false, err
		}
		changed, err := b.readAuthoritiesChanged(ctx, owner, authorities)
		if changed || err != nil {
			return changed, err
		}
	}
	if dependency.Request.View == passMenuInvitations && dependency.Invitations == nil {
		return true, nil
	}
	read := agent.RegistrationReadResult{Request: dependency.Request}
	if identity := dependency.Booking; identity != nil {
		read.Booking = &passbooking.Booking{
			Event:     identity.Event,
			Owner:     identity.Owner,
			Version:   identity.Version,
			CreatedAt: identity.CreatedAt,
		}
	}
	for _, identity := range dependency.Invitations {
		read.Invitations = append(
			read.Invitations,
			passbooking.Invitation{
				From:      passbooking.Contact{Owner: identity.Owner},
				Version:   identity.Version,
				CreatedAt: identity.CreatedAt,
			},
		)
	}
	if err := b.reauthorizeRegistrationRead(ctx, owner, &read); err != nil {
		return false, err
	}
	return read.Error != "", nil
}
