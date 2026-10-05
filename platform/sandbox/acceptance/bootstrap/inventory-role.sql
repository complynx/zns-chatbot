-- Bootstrap administrator only. Inventory cannot write fixture or clock state.
\getenv inventory_password ZNS_REGISTRATION_INVENTORY_PASSWORD
CREATE ROLE zns_inventory LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'inventory_password';
GRANT CONNECT ON DATABASE synthetic_qa_zns_registration_fixture TO zns_inventory;
GRANT pg_read_all_stats TO zns_inventory;
ALTER ROLE zns_inventory SET default_transaction_read_only = on;
ALTER ROLE zns_app IN DATABASE synthetic_qa_zns_registration_fixture SET client_connection_check_interval = '100ms';
ALTER ROLE zns_meter IN DATABASE synthetic_qa_zns_registration_fixture SET client_connection_check_interval = '100ms';
\unset inventory_password
