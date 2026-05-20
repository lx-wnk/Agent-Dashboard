#!/bin/bash
set -e
cd "$(dirname "$0")"
export GOWORK=off
go run -mod=mod entgo.io/ent/cmd/ent generate --target . ./schema
