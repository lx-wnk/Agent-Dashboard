package mcpapps

import (
	"encoding/json"
	"fmt"
	"strings"
)

// EnvAccountKey normalises an account name the way the mail server itself
// does (upper-case, every character outside A-Z0-9 replaced by an
// underscore), so the names the dashboard offers match the variables that
// server actually reads. Verified against imap-mcp-server@2.0.0.
func EnvAccountKey(accountName string) string {
	var b strings.Builder
	b.Grow(len(accountName))
	for _, r := range strings.ToUpper(accountName) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	return b.String()
}

// SecretNamesForAccounts fills each template's {ACCOUNT} placeholder with the
// normalised name of every account, template by template so the order the
// preset declares is the order the operator sees. An account whose name
// normalises to underscores only carries no usable key and is skipped.
func SecretNamesForAccounts(templates []string, accountNames []string) []string {
	out := make([]string, 0, len(templates)*len(accountNames))
	for _, tpl := range templates {
		for _, name := range accountNames {
			key := EnvAccountKey(name)
			if strings.Trim(key, "_") == "" {
				continue
			}
			out = append(out, strings.ReplaceAll(tpl, "{ACCOUNT}", key))
		}
	}
	return out
}

// AccountNames reads the account names out of what imap_list_accounts
// answers. The tool returns text content holding JSON — see
// docs/superpowers/notes/2026-09-17-imap-list-accounts-shape.md — and an
// empty list is a fresh install, not a failure.
func AccountNames(raw json.RawMessage) ([]string, error) {
	var body struct {
		Accounts []struct {
			Name string `json:"name"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("mcpapps: parse account list: %w", err)
	}
	names := make([]string, 0, len(body.Accounts))
	for _, a := range body.Accounts {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}
	return names, nil
}
