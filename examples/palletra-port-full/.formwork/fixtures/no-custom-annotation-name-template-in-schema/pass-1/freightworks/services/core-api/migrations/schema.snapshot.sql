CREATE TABLE palletra.projects (
    id uuid PRIMARY KEY
);

-- The sibling custom_page_presets table is a separate, live concern and does
-- not carry the banned token.
CREATE TABLE palletra.custom_page_presets (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL
);
