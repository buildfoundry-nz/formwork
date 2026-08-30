CREATE TABLE palletra.page_gauges (
    id uuid PRIMARY KEY,
    principal_kind palletra.metric_kind NOT NULL,
    anno_kind palletra.annotation_type
);
