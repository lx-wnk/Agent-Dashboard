# What `imap_list_accounts` returns

Verified on 2026-09-17 against the published `imap-mcp-server@2.0.0`, driven over stdio with a
throwaway `HOME` so nothing touched a real mail configuration.

## The tool

`imap_list_accounts` takes no arguments:

```json
{ "name": "imap_list_accounts", "inputSchema": { "type": "object", "properties": {} } }
```

## The result

It answers with **text content holding JSON**, not structured content. On a fresh install with no
account configured:

```json
{ "result": { "content": [ { "type": "text", "text": "{\n  \"accounts\": []\n}" } ] }, "jsonrpc": "2.0", "id": 3 }
```

A caller therefore has to parse the first text content's body. An empty list is a success, not an
error: a fresh install is exactly this state, and the panel must treat it as "no accounts yet"
rather than as a failure.

Each account in that list carries (`dist/index.js:2588-2608`):

```json
{ "id": "...", "name": "...", "host": "...", "port": 993, "user": "...", "tls": true }
```

Passwords are never returned.

## Which field becomes `{ACCOUNT}`

The **`name`**, not the `id`. The server builds its environment variable names from the account's
name (`dist/index.js:143-148`, identical copy in `dist/setup.js:26-31`):

```js
function envAccountKey(accountName) {
  return accountName.toUpperCase().replace(/[^A-Z0-9]/g, "_");
}
function envVarName(accountName, suffix) {
  return `IMAP_MCP_ACCOUNT_${envAccountKey(accountName)}${suffix}`;
}
```

and `account.name` is what every call site passes (`dist/index.js:152`, `:159`, `:164`, `:1684`).

The four suffixes (`dist/index.js:137-142`):

| Suffix | Holds |
| --- | --- |
| `_IMAP_USERNAME` | IMAP username |
| `_IMAP_PASSWORD` | IMAP password |
| `_SMTP_USERNAME` | SMTP username |
| `_SMTP_PASSWORD` | SMTP password |

## The derivation rule

`IMAP_MCP_ACCOUNT_` + upper-cased `name` with every character outside `[A-Z0-9]` replaced by `_` +
the suffix. Worked examples against the real implementation:

| Account `name` | Key | Password variable |
| --- | --- | --- |
| `work@example.com` | `WORK_EXAMPLE_COM` | `IMAP_MCP_ACCOUNT_WORK_EXAMPLE_COM_IMAP_PASSWORD` |
| `Work Gmail` | `WORK_GMAIL` | `IMAP_MCP_ACCOUNT_WORK_GMAIL_IMAP_PASSWORD` |

The second matches the table the package's own README prints, which is the cross-check that the
rule was read correctly.

## Consequences for the implementation

- The dashboard derives names from `name`, so two accounts whose names normalise to the same key
  collide. The panel shows the derived name, so a collision is visible rather than silent.
- An account whose name contains no alphanumeric character normalises to a string of underscores;
  that is a degenerate case the derivation skips.
- Credentials are "environment-managed" only when the account carries an empty string for them;
  the server then refuses to connect and names the missing variables in its error. That error text
  is the operator's fallback if the derivation ever disagrees with the server.
