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
DELETE FROM atlas_assets WHERE organization_id = :'org';
DELETE FROM atlas_models WHERE organization_id = :'org';
DELETE FROM people_location_references WHERE organization_id = :'org';
DELETE FROM people_location_reference_types WHERE organization_id = :'org';
DELETE FROM people_asset_assignments WHERE organization_id = :'org';
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
go run ./cmd/campus-seed
echo "Campus demo is ready. Refresh the web app at http://localhost:5173"
