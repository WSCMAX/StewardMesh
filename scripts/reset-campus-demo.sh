#!/usr/bin/env bash
# Wipes Riverside Community College demo data, then reloads it with campus-seed.
set -euo pipefail

repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repository_root}"

if [[ -f "${repository_root}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${repository_root}/.env"
  set +a
fi

ORG="${STEWARDMESH_ORGANIZATION_ID:-demo-campus}"
DB_URL="${STEWARDMESH_DATABASE_URL:-postgres://stewardmesh:stewardmesh@localhost:5432/stewardmesh?sslmode=disable}"

if [[ "${ORG}" != demo-* ]]; then
  echo "Campus demo reset requires STEWARDMESH_ORGANIZATION_ID to start with demo- (got ${ORG})." >&2
  exit 1
fi

echo "Resetting campus demo data for organization: ${ORG}"

psql "$DB_URL" -v ON_ERROR_STOP=1 -v org="$ORG" <<'SQL'
BEGIN;
DELETE FROM signal_deliveries WHERE organization_id = :'org';
DELETE FROM signal_subscriptions WHERE organization_id = :'org';
DELETE FROM signal_alert_history WHERE organization_id = :'org';
DELETE FROM signal_alerts WHERE organization_id = :'org';
DELETE FROM signal_rules WHERE organization_id = :'org';
DELETE FROM reach_delivery_attempts WHERE organization_id = :'org';
DELETE FROM reach_messages WHERE organization_id = :'org';
DELETE FROM reach_subscriber_groups WHERE organization_id = :'org';
DELETE FROM reach_templates WHERE organization_id = :'org';
DELETE FROM reach_providers WHERE organization_id = :'org';
DELETE FROM reach_provider_tests WHERE organization_id = :'org';
DELETE FROM bridge_mcp_confirmations WHERE organization_id = :'org';
DELETE FROM bridge_oauth_grants WHERE organization_id = :'org';
DELETE FROM bridge_oauth_authorization_codes WHERE organization_id = :'org';
DELETE FROM bridge_oauth_authorization_requests WHERE organization_id = :'org';
DELETE FROM bridge_oauth_clients WHERE organization_id = :'org';
DELETE FROM exchange_packages WHERE organization_id = :'org';
DELETE FROM labels_assignments WHERE organization_id = :'org';
DELETE FROM labels_definitions WHERE organization_id = :'org';
DELETE FROM threads_goal_links WHERE organization_id = :'org';
DELETE FROM threads_tag_rules WHERE organization_id = :'org';
DELETE FROM threads_goals WHERE organization_id = :'org';
DELETE FROM threads_tags WHERE organization_id = :'org';
DELETE FROM directory_managed_memberships WHERE organization_id = :'org';
DELETE FROM directory_managed_groups WHERE organization_id = :'org';
DELETE FROM directory_import_attempts WHERE organization_id = :'org';
DELETE FROM directory_import_items WHERE organization_id = :'org';
DELETE FROM directory_import_mappings WHERE organization_id = :'org';
DELETE FROM guard_sessions WHERE organization_id = :'org';
DELETE FROM guard_saml_requests WHERE organization_id = :'org';
DELETE FROM guard_role_assignments WHERE organization_id = :'org';
DELETE FROM guard_role_policy_bundles WHERE organization_id = :'org';
DELETE FROM guard_role_permissions WHERE organization_id = :'org';
DELETE FROM guard_roles WHERE organization_id = :'org';
DELETE FROM guard_policy_bundle_permissions WHERE organization_id = :'org';
DELETE FROM guard_policy_bundles WHERE organization_id = :'org';
DELETE FROM guard_external_identities WHERE organization_id = :'org';
DELETE FROM guard_accounts WHERE organization_id = :'org';
DELETE FROM guard_resource_ownership WHERE organization_id = :'org';
DELETE FROM stack_assignments WHERE organization_id = :'org';
DELETE FROM stack_installations WHERE organization_id = :'org';
DELETE FROM stack_licenses WHERE organization_id = :'org';
DELETE FROM stack_versions WHERE organization_id = :'org';
DELETE FROM stack_products WHERE organization_id = :'org';
DELETE FROM ledger_costs WHERE organization_id = :'org';
DELETE FROM ledger_commitments WHERE organization_id = :'org';
DELETE FROM ledger_purchase_orders WHERE organization_id = :'org';
DELETE FROM ledger_contracts WHERE organization_id = :'org';
DELETE FROM ledger_budgets WHERE organization_id = :'org';
DELETE FROM ledger_vendors WHERE organization_id = :'org';
DELETE FROM vault_blobs WHERE organization_id = :'org';
DELETE FROM audit_events WHERE organization_id = :'org';
DELETE FROM atlas_asset_lifecycle_events WHERE organization_id = :'org';
DELETE FROM atlas_asset_identifiers WHERE organization_id = :'org';
DELETE FROM atlas_catalog_prices WHERE organization_id = :'org';
DELETE FROM atlas_catalog_upgrade_paths WHERE organization_id = :'org';
DELETE FROM atlas_catalog_configurations WHERE organization_id = :'org';
DELETE FROM horizon_plan_versions WHERE organization_id = :'org';
DELETE FROM horizon_plans WHERE organization_id = :'org';
DELETE FROM horizon_kind_defaults WHERE organization_id = :'org';
DELETE FROM horizon_replacement_plan_assets WHERE organization_id = :'org';
DELETE FROM horizon_replacement_plans WHERE organization_id = :'org';
DELETE FROM atlas_assets WHERE organization_id = :'org';
DELETE FROM atlas_models WHERE organization_id = :'org';
DELETE FROM people_location_references WHERE organization_id = :'org';
DELETE FROM people_location_reference_types WHERE organization_id = :'org';
DELETE FROM people_asset_assignments WHERE organization_id = :'org';
DELETE FROM people_checkout_group_members WHERE organization_id = :'org';
DELETE FROM people_bulk_checkouts WHERE organization_id = :'org';
DELETE FROM people_checkout_groups WHERE organization_id = :'org';
DELETE FROM people_identities WHERE organization_id = :'org';
DELETE FROM people_rooms WHERE organization_id = :'org';
DELETE FROM people_buildings WHERE organization_id = :'org';
DELETE FROM people_departments WHERE organization_id = :'org';
DELETE FROM people_sites WHERE organization_id = :'org';
COMMIT;
SQL

echo "Campus demo data cleared. Loading Riverside Community College demo (this takes several minutes)..."
export STEWARDMESH_ORGANIZATION_ID="${ORG}"
export STEWARDMESH_SEED_CAMPUS=true
if [[ -z "${STEWARDMESH_REACH_ENDPOINTS_FILE:-}" ]]; then
  export STEWARDMESH_REACH_ENDPOINTS_FILE="${repository_root}/deploy/reach-endpoints.example.json"
fi
go run -tags campusdemo ./cmd/campus-seed
echo "Campus demo is ready. Refresh the web app at http://localhost:5173"
echo "Demo credentials: tmp/campus-test-users.env"
