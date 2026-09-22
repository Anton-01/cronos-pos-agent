//go:build darwin

package main

// EnsureStartMenuShortcut is a no-op on macOS: there is no Start menu, and the
// agent is launched from /Applications or by its LaunchAgent, both of which
// Spotlight already indexes without anything being created for them.
func EnsureStartMenuShortcut() {}

// startMenuShortcutStatusLabel reports what the self-test ticket prints for the
// shortcut on a platform that does not have one.
func startMenuShortcutStatusLabel() string {
	return "no aplica (macOS)"
}

// autostartStatusLabel reports whether the agent is set to start with the
// session, for the self-test ticket.
func autostartStatusLabel() string {
	if isAutostartEnabled() {
		return "Sí (LaunchAgent)"
	}
	return "No"
}
