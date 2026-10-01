package config

import (
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

// Delivery configures conservative pacing. Telegram cooldowns can extend it.
type Delivery struct {
	BotInterval          time.Duration `yaml:"bot_interval"           json:"bot_interval"`
	ChatInterval         time.Duration `yaml:"chat_interval"          json:"chat_interval"`
	CooldownFallback     time.Duration `yaml:"cooldown_fallback"      json:"cooldown_fallback"`
	UncertaintyRetryBase time.Duration `yaml:"uncertainty_retry_base" json:"uncertainty_retry_base"`
}

func defaultDelivery() Delivery {
	const botInterval = 50 * time.Millisecond
	const cooldownFallback = 30 * time.Second
	return Delivery{BotInterval: botInterval, ChatInterval: time.Second, CooldownFallback: cooldownFallback,
		UncertaintyRetryBase: delivery.DefaultUncertaintyRetryBase}
}

// DeliverySettings binds pacing to the configured bot, never to a request actor.
func (c Config) DeliverySettings() (delivery.Settings, error) {
	botID, err := c.LegacyOrderBotID()
	if err != nil {
		return delivery.Settings{}, err
	}
	if c.Delivery.UncertaintyRetryBase <= 0 {
		return delivery.Settings{}, delivery.ErrSettings
	}
	settings := delivery.Settings{
		BotID: botID, BotInterval: c.Delivery.BotInterval,
		ChatInterval: c.Delivery.ChatInterval, Fallback: c.Delivery.CooldownFallback,
		UncertaintyRetryBase: c.Delivery.UncertaintyRetryBase,
	}
	if validationErr := settings.Validate(); validationErr != nil {
		return delivery.Settings{}, validationErr
	}
	return settings, nil
}
