# layered-review

The audit skill drives the review loop.

## Overview

Reviewers run several cycles. But the section header the SessionStart hook
extracts its slice from has been renamed, so the injected slice is empty.

## Failure modes

- divergence
- duplication
- deferral
