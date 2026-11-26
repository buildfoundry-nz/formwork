CREATE TABLE sample.annotations (
    id uuid NOT NULL,
    segment_id uuid NOT NULL
);

CREATE TABLE sample.annotation_gauges (
    id uuid NOT NULL,
    annotation_id uuid NOT NULL
);

ALTER TABLE ONLY sample.annotations
    ADD CONSTRAINT markers_pkey PRIMARY KEY (id);

ALTER TABLE ONLY sample.annotation_gauges
    ADD CONSTRAINT annotation_gauges_pkey PRIMARY KEY (id);

CREATE INDEX idx_annotation_gauges_annotation_id ON sample.annotation_gauges USING btree (annotation_id);

ALTER TABLE ONLY sample.annotation_gauges
    ADD CONSTRAINT annotation_gauges_annotation_id_fkey FOREIGN KEY (annotation_id) REFERENCES sample.annotations(id) ON DELETE CASCADE;
