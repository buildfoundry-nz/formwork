#!/bin/sh
go test ./... | head -1 # want: no-sigpipe-producer-grep
