//go:build !unix

package switchfs

// oNoFollow is the host's O_NOFOLLOW open flag. This host's syscall package
// has none, so there is nothing for a trailing slash to override.
const oNoFollow = 0
