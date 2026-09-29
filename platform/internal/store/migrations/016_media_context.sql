ALTER TABLE bot.media_intake ADD COLUMN last_action text NOT NULL DEFAULT 'received';
ALTER TABLE bot.media_intake ADD COLUMN last_origin text NOT NULL DEFAULT 'agent';
