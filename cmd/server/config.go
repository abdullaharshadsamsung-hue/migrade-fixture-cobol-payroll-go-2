package main

import "migrated-app/internal/config"

// loadConfig delegates to the shared config package; kept as a tiny shim
// so main.go stays focused on server lifecycle.
func loadConfig() (*config.Config, error) {
	return config.Load()
}
