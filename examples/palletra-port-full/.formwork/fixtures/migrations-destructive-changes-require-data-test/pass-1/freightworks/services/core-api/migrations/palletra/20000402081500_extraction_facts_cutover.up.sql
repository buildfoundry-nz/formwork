ALTER TABLE palletra.extraction_facts DROP COLUMN legacy_payload;

ALTER TABLE palletra.extraction_facts ALTER COLUMN revision_id SET NOT NULL;
