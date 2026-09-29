package appclient

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// SubmitKnowledgeProposal carries an owner-bound manual callback snapshot.
// It is not exposed by Client or by a conversational tool.
func (c Host) SubmitKnowledgeProposal(
	ctx context.Context,
	owner string,
	input knowledge.Submission,
) (knowledge.Result, error) {
	if c.LocalKnowledge != nil {
		return hostKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) (knowledge.Result, error) {
			return s.SubmitProposal(ctx, actor, input)
		})
	}

	if c.UserToken == nil {
		return knowledge.Result{}, identity.ErrZitadelIdentity
	}
	token, err := c.UserToken(ctx, owner)
	if err != nil {
		return knowledge.Result{}, err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return knowledge.Result{}, err
	}
	var result knowledge.Result
	err = requestAuthorizedLimit(
		ctx,
		c.Base,
		c.HTTP,
		token,
		c.Signer.DerivedMutationToken(owner),
		http.MethodPost,
		"/internal/knowledge/submit",
		body,
		&knowledgeEnvelope{&result},
		knowledgeResponseBytes,
	)
	result.RestoreReadAuthorities()
	return result, err
}
