-- Claim Reach delivery attempts before external side effects so concurrent
-- workers cannot send the same message. Requirements: REQ-REACH-001,
-- REQ-SIGNALS-001. Feature: messaging.delivery. GitHub: #12.

ALTER TABLE reach_messages
    ADD COLUMN claim_token TEXT,
    ADD COLUMN claimed_at TIMESTAMPTZ,
    ADD CONSTRAINT reach_messages_claim_pair CHECK ((claim_token IS NULL) = (claimed_at IS NULL)),
    ADD CONSTRAINT reach_messages_claim_token_valid CHECK (
        claim_token IS NULL OR claim_token ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
    );

CREATE INDEX reach_messages_claim_recovery_idx
    ON reach_messages(organization_id, claimed_at, id)
    WHERE claim_token IS NOT NULL;
