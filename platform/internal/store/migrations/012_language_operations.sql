CREATE TABLE core.language_operations (
  owner text NOT NULL REFERENCES core.users(id),
  operation_key text NOT NULL CHECK (octet_length(operation_key) BETWEEN 1 AND 128),
  language text NOT NULL,
  initialize boolean NOT NULL,
  PRIMARY KEY (owner, operation_key)
);
