package ent

//go:generate env GOWORK=off go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/upsert --target . ./schema
