#!/bin/sh
out=$(go test ./...)
printf '%s\n' "$out" | sed -n '1p'
