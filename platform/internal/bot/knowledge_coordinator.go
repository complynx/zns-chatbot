package bot

import (
	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
)

const knowledgePendingFilter = "pending_filter"
const knowledgeReviewCard = "review_card"

func (b *Bot) knowledgeCoordinator() interaction.KnowledgeCoordinator {
	classifier, _ := b.Model.(agent.KnowledgeClassifier)
	return interaction.KnowledgeCoordinator{
		Client:   b.API,
		Host:     b.Host,
		Store:    interaction.Store{DB: b.DB},
		Assessor: agenthost.KnowledgeAssessor{Classifier: classifier, Authority: b.Host},
	}
}
