package ent

// --feature sql/upsert is load-bearing: it generates ent's OnConflict* builders that db/repo depends on. Regenerating without it strips them and breaks the repo layer.
//go:generate env GOWORK=off go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/upsert --target . ./schema
