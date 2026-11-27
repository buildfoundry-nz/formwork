CREATE TABLE sample.page_gauges (
    id uuid PRIMARY KEY,
    principal_kind sample.metric_kind NOT NULL,
    anno_kind sample.annotation_type
);
