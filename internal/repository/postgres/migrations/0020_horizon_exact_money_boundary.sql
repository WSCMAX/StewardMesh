-- Keep Horizon minor-unit values exactly representable across Go, PostgreSQL, JSON, and browsers.
-- Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

ALTER TABLE horizon_plans
    ADD CONSTRAINT horizon_plans_exact_money_check
    CHECK (replacement_cost_minor <= 9007199254740991);

ALTER TABLE horizon_plan_versions
    ADD CONSTRAINT horizon_plan_versions_exact_money_check
    CHECK (replacement_cost_minor <= 9007199254740991);
