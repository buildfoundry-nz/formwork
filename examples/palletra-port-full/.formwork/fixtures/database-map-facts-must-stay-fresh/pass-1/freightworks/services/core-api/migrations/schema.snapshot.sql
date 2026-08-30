CREATE TABLE palletra.projects (id uuid PRIMARY KEY);
CREATE TABLE palletra.pages (id uuid PRIMARY KEY);
ALTER TABLE palletra.projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE palletra.pages ENABLE ROW LEVEL SECURITY;
