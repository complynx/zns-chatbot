package task

import (
	"context"
	"sync"
	"time"

	"github.com/complynx/zns-chatbot/internal/db"
)

const typingRefresh = 4 * time.Second
const typingConcurrency = 8

type typingPage func(context.Context, int64) ([]int64, error)
type typingNeeded func(context.Context, int64) (bool, error)
type typingSend func(context.Context, int64) error

type typingActivity struct {
	last     time.Time
	inflight bool
}
type typingResult struct {
	id     int64
	active bool
	err    error
}
type typingLoop struct {
	known    map[int64]*typingActivity
	results  chan typingResult
	workers  sync.WaitGroup
	now      time.Time
	after    int64
	inflight int
}

// runTyping is cosmetic and independent of worker retries. Known active chats
// refresh independently of discovery pages; failed actions are also throttled.
func runTyping(
	ctx context.Context, ticks <-chan time.Time,
	page typingPage, needed typingNeeded, send typingSend,
) {
	loop := typingLoop{known: make(map[int64]*typingActivity), results: make(chan typingResult, typingConcurrency)}
	defer loop.workers.Wait()
	for {
		select {
		case <-ctx.Done():
			return
		case completed := <-loop.results:
			loop.inflight--
			if completed.err == nil && !completed.active {
				delete(loop.known, completed.id)
			} else {
				loop.known[completed.id].inflight = false
			}
		case tick, ok := <-ticks:
			if !ok {
				return
			}
			loop.now = tick
			loop.discover(ctx, page)
		}
		loop.pump(ctx, needed, send)
	}
}

func (l *typingLoop) discover(ctx context.Context, page typingPage) {
	read, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	chats, err := page(read, l.after)
	if err != nil {
		return
	}
	if len(chats) < db.TypingPageSize {
		l.after = 0
	} else {
		l.after = chats[len(chats)-1]
	}
	for _, id := range chats {
		if l.known[id] == nil {
			l.known[id] = &typingActivity{}
		}
	}
}

func (l *typingLoop) pump(ctx context.Context, needed typingNeeded, send typingSend) {
	for id, chat := range l.known {
		if ctx.Err() != nil || l.inflight == typingConcurrency {
			return
		}
		if chat.inflight || l.now.Sub(chat.last) < typingRefresh {
			continue
		}
		chat.last, chat.inflight = l.now, true
		l.inflight++
		l.workers.Add(1)
		go l.attempt(ctx, id, needed, send)
	}
}

func (l *typingLoop) attempt(ctx context.Context, id int64, needed typingNeeded, send typingSend) {
	defer l.workers.Done()
	attempt, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	active, err := needed(attempt, id)
	if err == nil && active && attempt.Err() == nil {
		_ = send(attempt, id)
	}
	select {
	case l.results <- typingResult{id, active, err}:
	case <-ctx.Done():
	}
}

func (s *Service) watchTyping(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	_, organization, _ := s.iam.DeploymentScope()
	runTyping(ctx, ticker.C,
		func(ctx context.Context, after int64) ([]int64, error) {
			return s.store.TypingChats(ctx, s.options.BotID, organization, after)
		},
		func(ctx context.Context, id int64) (bool, error) {
			return s.store.NeedsTyping(ctx, s.options.BotID, organization, id)
		}, s.tg.SendChatAction)
}
