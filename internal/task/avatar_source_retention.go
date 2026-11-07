package task

import (
	"context"
	"time"
)

func (s *Service) expireAvatarSources(ctx context.Context) {
	attempt, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	next, err := s.store.ExpireAvatarSources(attempt, s.notificationScope(), s.avatarSourceExpiryCursor)
	if err == nil {
		s.avatarSourceExpiryCursor = next
	} else if ctx.Err() == nil {
		s.record(ctx, "avatar_source_expiry", metricError)
		s.logger.WarnContext(ctx, "avatar source expiry unavailable", "class", "avatar_source_expiry")
	}
}
