package descriptor

import (
	"context"
	"time"
)

func descriptorActor(ctx context.Context, a arguments) (string, error) {
	if a.machine == nil {
		return token(a.actorToken)
	}
	credential, err := a.machine.Token(ctx, procedureTimeout+30*time.Second)
	if err != nil {
		return "", err
	}
	return credential.Token, nil
}
