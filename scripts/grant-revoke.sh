#!/usr/bin/env bash

set -exo pipefail

BATON_GRANT=scope:whitelabel.read:assigned:teammate:marcus.goldsmith@conductorone.com
BATON_ENTITLEMENT=scope:whitelabel.read:assigned
BATON_PRINCIPAL=marcus.goldsmith@conductorone.com
BATON_PRINCIPAL_TYPE=teammate

if [ -z "$BATON_SENDGRID" ]; then
  echo "BATON_SENDGRID not set. using baton-ldap"
  BATON_SENDGRID=baton-sendgrid
fi
if [ -z "$BATON" ]; then
  echo "BATON not set. using baton"
  BATON=baton
fi

# Error on unbound variables now that we've set BATON & BATON_SENDGRID
set -u

# Grant entitlement
$BATON_SENDGRID --grant-entitlement="$BATON_ENTITLEMENT" --grant-principal="$BATON_PRINCIPAL" --grant-principal-type="$BATON_PRINCIPAL_TYPE" --log-level debug

# Check for grant before revoking
$BATON_SENDGRID
$BATON grants --entitlement="$BATON_ENTITLEMENT" --output-format=json | jq --exit-status ".grants[] | select( .principal.id.resource == \"$BATON_PRINCIPAL\" )"

## Revoke grant
$BATON_SENDGRID --revoke-grant="$BATON_GRANT"

# Revoke already-revoked grant
$BATON_SENDGRID --revoke-grant="$BATON_GRANT"

# Check grant was revoked
$BATON_SENDGRID
$BATON grants --entitlement="$BATON_ENTITLEMENT" --output-format=json | jq --exit-status "if .grants then [ .grants[] | select( .principal.id.resource == \"$BATON_PRINCIPAL\" ) ] | length == 0 else . end"

# Re-grant entitlement
$BATON_SENDGRID --grant-entitlement="$BATON_ENTITLEMENT" --grant-principal="$BATON_PRINCIPAL" --grant-principal-type="$BATON_PRINCIPAL_TYPE"

# Check grant was re-granted
$BATON_SENDGRID
$BATON grants --entitlement="$BATON_ENTITLEMENT" --output-format=json | jq --exit-status ".grants[] | select( .principal.id.resource == \"$BATON_PRINCIPAL\" )"