# Extraction eval harness

The eval store lives OUTSIDE the repo (EXTRACTION_EVAL_DIR / a private GCS
bucket). The in-repo mount `eval-data/` is .gitignored and must stay empty in
git. This doc is fine to commit — it is not eval data.
