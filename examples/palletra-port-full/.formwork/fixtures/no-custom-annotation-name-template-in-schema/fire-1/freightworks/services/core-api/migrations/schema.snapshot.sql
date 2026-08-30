CREATE TABLE palletra.projects (
    id uuid PRIMARY KEY
);

CREATE TABLE palletra.custom_annotation_label_presets ( -- want: no-custom-annotation-name-template-in-schema
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    name text NOT NULL
);
