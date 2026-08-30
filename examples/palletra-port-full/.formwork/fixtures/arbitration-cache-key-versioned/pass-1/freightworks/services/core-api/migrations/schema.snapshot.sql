CREATE TABLE palletra.sku_match_arbitrations (
    sku_id uuid NOT NULL,
    match_candidate_hash text NOT NULL,
    arbitrator_prompt_version integer NOT NULL,
    arbitrator_model text NOT NULL,
    verdict text
);

CREATE UNIQUE INDEX uq_sma_cache_key ON palletra.sku_match_arbitrations USING btree (sku_id, match_candidate_hash, arbitrator_prompt_version, arbitrator_model);
