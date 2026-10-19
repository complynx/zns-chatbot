-- +goose Up
CREATE TABLE shuttle_deployment (
 id boolean PRIMARY KEY DEFAULT true CHECK(id),
 bot_id bigint NOT NULL CHECK(bot_id>0),
 organization_id text NOT NULL CHECK(organization_id<>''),
 issuer text NOT NULL CHECK(issuer<>''),
 created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE shuttle_deployment;
