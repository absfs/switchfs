//go:build !darwin

package switchfs

// hostMajorVersion returns the host OS major version where a test needs
// it; only macOS reports one.
func hostMajorVersion() int { return 0 }
