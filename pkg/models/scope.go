package models

import (
	"strings"
)

// - account
//   :read
//   :address
//   :modify
//   :delete
// - addresses
//   :read
//   :modify
//   :delete
// - applications
//   :read
//   :modify
//   :delete
// - emails
//   :send
//   :read
//   :modify
//   :delete
// - keys
//   :read
//   :modify
//   :delete
// - labels
//   :read
//   :modify
//   :delete
// - resources
//   :create
//   :read
//   :modify
//   :delete
// - threads
//   :read
//   :modify
//   :delete
// - tokens
//   :read
//   :oauth
//   :logout
//   :modify
//   :delete

var Scopes = map[string]struct{}{
	"password_grant":      {},
	"admin":               {},
	"account":             {},
	"account:read":        {},
	"account:address":     {},
	"account:modify":      {},
	"account:delete":      {},
	"addresses":           {},
	"addresses:read":      {},
	"addresses:modify":    {},
	"addresses:delete":    {},
	"applications":        {},
	"applications:read":   {},
	"applications:modify": {},
	"applications:delete": {},
	"emails":              {},
	"emails:send":         {},
	"emails:read":         {},
	"emails:modify":       {},
	"emails:delete":       {},
	"keys":                {},
	"keys:read":           {},
	"labels":              {},
	"labels:read":         {},
	"labels:modify":       {},
	"labels:delete":       {},
	"resources":           {},
	"resources:read":      {},
	"resources:modify":    {},
	"resources:delete":    {},
	"threads":             {},
	"threads:read":        {},
	"threads:modify":      {},
	"threads:delete":      {},
	"tokens":              {},
	"tokens:read":         {},
	"tokens:oauth":        {},
	"tokens:logout":       {},
	"tokens:modify":       {},
	"tokens:delete":       {},
}

func InScope(scope []string, what []string) bool {
	// Transform scope into a hashmap
	hm := map[string]struct{}{}
	for _, x := range scope {
		hm[x] = struct{}{}
	}

	// Check the scope contents
	for _, x := range what {
		found := false

		if _, ok := hm["password_grant"]; x != "admin" && ok {
			return true
		}

		if _, ok := hm[x]; ok {
			found = true
		} else {
			if strings.Contains(x, ":") {
				parts := strings.SplitN(x, ":", 2)

				if _, ok := hm[parts[0]]; ok {
					found = true
				}
			}
		}

		if !found {
			return false
		}
	}

	return true
}
