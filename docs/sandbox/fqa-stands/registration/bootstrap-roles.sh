#!/bin/sh
# Sourced only during the allocated fresh PostgreSQL cluster initialization.
set +x
set -eu
registration_bootstrap_cleanup() {
  unset ZNS_REGISTRATION_APP_PASSWORD ZNS_REGISTRATION_METER_PASSWORD ZNS_REGISTRATION_INVENTORY_PASSWORD ZNS_REGISTRATION_FAKE_PASSWORD ZNS_REGISTRATION_OPERATOR_PASSWORD
}
trap registration_bootstrap_cleanup EXIT
trap 'registration_bootstrap_cleanup; exit 1' HUP INT TERM
test "${POSTGRES_USER:-postgres}" = postgres
test "$POSTGRES_DB" = synthetic_qa_zns_registration_fixture
ZNS_REGISTRATION_APP_PASSWORD=$(cat /run/secrets/app_password)
ZNS_REGISTRATION_METER_PASSWORD=$(cat /run/secrets/meter_password)
ZNS_REGISTRATION_INVENTORY_PASSWORD=$(cat /run/secrets/inventory_password)
ZNS_REGISTRATION_FAKE_PASSWORD=$(cat /run/secrets/fake_password)
ZNS_REGISTRATION_OPERATOR_PASSWORD=$(cat /run/secrets/operator_password)
test -n "$ZNS_REGISTRATION_APP_PASSWORD"
test -n "$ZNS_REGISTRATION_METER_PASSWORD"
test -n "$ZNS_REGISTRATION_INVENTORY_PASSWORD"
test -n "$ZNS_REGISTRATION_FAKE_PASSWORD"
test -n "$ZNS_REGISTRATION_OPERATOR_PASSWORD"
export ZNS_REGISTRATION_APP_PASSWORD ZNS_REGISTRATION_METER_PASSWORD ZNS_REGISTRATION_INVENTORY_PASSWORD
export ZNS_REGISTRATION_FAKE_PASSWORD ZNS_REGISTRATION_OPERATOR_PASSWORD
psql --no-psqlrc --single-transaction -v ON_ERROR_STOP=1 -v ECHO=none --username postgres --dbname "$POSTGRES_DB" \
  -f /bootstrap/product-roles.sql -f /bootstrap/inventory-role.sql
registration_bootstrap_cleanup
trap - EXIT HUP INT TERM
