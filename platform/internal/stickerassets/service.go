// Package stickerassets resolves Telegram artwork to context-free cached descriptions.
package stickerassets

import (
	"context"
	"errors"
	"fmt"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/stickercache"
	"github.com/complynx/zns-chatbot/platform/internal/stickermedia"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// MaxAssets bounds model work and the description envelope per message. Repeated
// occurrences share one observation and count as one popularity hit per message.
const MaxAssets = 8

type Cache interface {
	Get(context.Context, stickercache.Key, stickercache.Processor) (string, error)
}

// Decoder must be a client of the isolated normalization service, never an
// in-process native decoder in the bot or model service.
type Decoder interface {
	Normalize(context.Context, []byte, stickermedia.Format) ([]stickermedia.Frame, error)
}

type Service struct {
	Telegram telegram.Client
	Cache    Cache
	Decoder  Decoder
	Model    agent.Model
}

type asset struct {
	key     stickercache.Key
	sticker *telegram.Sticker
	spans   []agent.AssetOccurrence
}

// Describe runs after user authorization. Failures are explicit unavailable
// observations; only successful artwork descriptions enter the shared cache.
func (s Service) Describe(ctx context.Context, message telegram.Message) (agent.AssetContext, error) {
	assets, err := messageAssets(message)
	if err != nil {
		return agent.AssetContext{}, err
	}
	result := agent.AssetContext{Items: make([]agent.AssetObservation, 0, min(len(assets), MaxAssets))}
	if len(assets) > MaxAssets {
		result.Omitted = len(assets) - MaxAssets
		assets = assets[:MaxAssets]
	}
	for _, item := range assets {
		observation := agent.AssetObservation{
			Kind:        string(item.key.Kind),
			Status:      "unavailable",
			Occurrences: item.spans,
		}
		if s.Cache != nil && s.Decoder != nil && s.Model != nil {
			description, describeErr := s.Cache.Get(
				ctx,
				item.key,
				func(ctx context.Context, _ stickercache.Key) (string, error) {
					return s.describe(ctx, item)
				},
			)
			if describeErr == nil {
				observation.Status, observation.Description = "described", description
			}
		}
		if err = ctx.Err(); err != nil {
			return agent.AssetContext{}, err
		}
		result.Items = append(result.Items, observation)
	}
	return result, nil
}

func messageAssets(message telegram.Message) ([]asset, error) {
	text, entities := message.Text, message.Entities
	if text == "" {
		text, entities = message.Caption, message.CaptionEntities
	}
	spans, err := telegram.CustomEmojiSpans(text, entities)
	if err != nil {
		return nil, err
	}
	result := make([]asset, 0, len(spans)+1)
	if message.Sticker != nil {
		result = append(result, asset{key: stickercache.Key{
			Kind:              stickercache.Sticker,
			AssetID:           message.Sticker.UniqueID,
			DescriptorVersion: agent.AssetDescriptionVersion,
		}, sticker: message.Sticker})
	}
	positions := make(map[string]int)
	for _, span := range spans {
		index, exists := positions[span.ID]
		if !exists {
			index = len(result)
			positions[span.ID] = index
			result = append(result, asset{key: stickercache.Key{Kind: stickercache.CustomEmoji,
				AssetID: span.ID, DescriptorVersion: agent.AssetDescriptionVersion}})
		}
		result[index].spans = append(result[index].spans, agent.AssetOccurrence{
			Offset: span.Offset, Length: span.Length, Placeholder: span.Text,
		})
	}
	return result, nil
}

func (s Service) describe(ctx context.Context, item asset) (string, error) {
	sticker := item.sticker
	if sticker == nil {
		stickers, err := s.Telegram.GetCustomEmojiStickers(ctx, []string{item.key.AssetID})
		if err != nil {
			return "", err
		}
		for index := range stickers {
			if stickers[index].CustomEmojiID == item.key.AssetID {
				if sticker != nil {
					return "", errors.New("duplicate custom emoji result")
				}
				sticker = &stickers[index]
			}
		}
	}
	format, err := assetFormat(sticker)
	if err != nil {
		return "", err
	}
	body, err := s.Telegram.Download(ctx, telegram.Document{FileID: sticker.FileID, Size: sticker.Size})
	if err != nil {
		return "", err
	}
	if len(body) > stickermedia.MaxInputBytes {
		return "", stickermedia.ErrLimit
	}
	frames, err := s.Decoder.Normalize(ctx, body, format)
	if err != nil {
		return "", fmt.Errorf("normalize sticker: %w", err)
	}
	attachments := make([]agent.Attachment, len(frames))
	for index, frame := range frames {
		attachments[index] = agent.Attachment{
			MIME:        "image/png",
			Body:        frame.PNG,
			TimestampMS: frame.Timestamp.Milliseconds(),
		}
	}
	return agent.DescribeAsset(ctx, s.Model, string(item.key.Kind), attachments)
}

func assetFormat(sticker *telegram.Sticker) (stickermedia.Format, error) {
	if sticker == nil || sticker.FileID == "" || sticker.Size < 0 || sticker.Size > stickermedia.MaxInputBytes ||
		(sticker.IsAnimated && sticker.IsVideo) {
		return "", errors.New("invalid sticker metadata")
	}
	if sticker.IsAnimated {
		return stickermedia.TGS, nil
	}
	if sticker.IsVideo {
		return stickermedia.WebM, nil
	}
	return stickermedia.WebP, nil
}
