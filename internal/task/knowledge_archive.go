package task

import (
	"context"
	"time"
)

// Archive uses the same bounded maintenance owner as other restart-safe repairs.
// It runs without IAM/Loom and never starts an unsolicited agent interaction.
func (s *Service) archiveEndedKnowledge(ctx context.Context) {
	attempt, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	next, err := s.store.ArchiveEndedKnowledge(attempt, s.notificationScope(), s.knowledgeArchiveCursor)
	if err == nil {
		s.knowledgeArchiveCursor = next
	} else if ctx.Err() == nil {
		s.record(ctx, "knowledge_archive", metricError)
		s.logger.WarnContext(ctx, "knowledge archival unavailable", "class", "knowledge_archive")
	}
}
