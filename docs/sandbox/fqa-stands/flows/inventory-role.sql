-- Synthetic stand only. This role never owns runtime sessions or writes data.
CREATE ROLE zns_inventory LOGIN PASSWORD 'fqa-flows-synthetic-inventory-only';
GRANT CONNECT ON DATABASE synthetic_qa_zns_fqa_flows TO zns_inventory;
GRANT pg_read_all_stats TO zns_inventory;
ALTER ROLE zns_inventory SET default_transaction_read_only = on;


-- Apply before any managed role connects; these affect fresh connections only.
ALTER ROLE zns_app IN DATABASE synthetic_qa_zns_fqa_flows SET client_connection_check_interval = '100ms';
ALTER ROLE zns_meter IN DATABASE synthetic_qa_zns_fqa_flows SET client_connection_check_interval = '100ms';
