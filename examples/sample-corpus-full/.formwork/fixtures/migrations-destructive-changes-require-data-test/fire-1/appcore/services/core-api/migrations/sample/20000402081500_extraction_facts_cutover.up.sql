ALTER TABLE sample.extraction_facts DROP COLUMN legacy_payload;

ALTER TABLE sample.extraction_facts ALTER COLUMN revision_id SET NOT NULL;
