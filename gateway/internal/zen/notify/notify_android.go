//go:build android

package notify

// Toast is a no-op on Android: there is no desktop notification channel, and
// the foreground-service notification is the app's only user-facing surface.
// The Android host keeps notifications disabled, so this is never reached; it
// exists so the package compiles for GOOS=android without pulling in the
// Linux notify-send implementation (android also satisfies the linux tag).
func Toast(title, body string) {}
