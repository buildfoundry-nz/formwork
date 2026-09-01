#!/bin/sh
rg -v foo -q bar || true # want: no-negated-searcher-quiet
