CREATE TABLE palletra.annotation_gauges (
  id uuid PRIMARY KEY,
  is_manual_flag boolean NOT NULL DEFAULT false, -- want: no-references-to-retired-override-flag
  adjusted_value numeric
);
