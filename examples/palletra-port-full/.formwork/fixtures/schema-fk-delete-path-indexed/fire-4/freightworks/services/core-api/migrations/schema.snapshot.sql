CREATE TABLE palletra.annotations (
    id uuid NOT NULL,
    segment_id uuid NOT NULL
);

CREATE TABLE palletra.annotation_gauges (
    id uuid NOT NULL,
    annotation_id uuid NOT NULL
);

ALTER TABLE ONLY palletra.annotations
    ADD CONSTRAINT markers_pkey PRIMARY KEY (id);

ALTER TABLE ONLY palletra.annotation_gauges
    ADD CONSTRAINT annotation_gauges_pkey PRIMARY KEY (id);

CREATE INDEX idx_annotation_gauges_annotation_id ON palletra.annotation_gauges USING btree (annotation_id);

ALTER TABLE ONLY palletra.annotation_gauges
    ADD CONSTRAINT annotation_gauges_annotation_id_fkey FOREIGN KEY (annotation_id) REFERENCES palletra.annotations(id) ON DELETE CASCADE;
ÿþ