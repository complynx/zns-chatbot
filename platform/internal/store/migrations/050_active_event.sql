-- Existing opaque paging tokens belong to the fixture event used before configuration.
ALTER TABLE bot.order_page_buttons ADD COLUMN event_id text NOT NULL DEFAULT 'sandbox-festival';
ALTER TABLE bot.order_pages ADD COLUMN event_id text NOT NULL DEFAULT 'sandbox-festival';
ALTER TABLE bot.order_pages DROP CONSTRAINT order_pages_pkey;
ALTER TABLE bot.order_pages ADD PRIMARY KEY(owner,scope,event_id);
