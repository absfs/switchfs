//go:build unix

package switchfs

import "syscall"

// oNoFollow is the host's O_NOFOLLOW open flag, which a trailing slash
// overrides: the slash requires a final symlink to be followed.
const oNoFollow = syscall.O_NOFOLLOW
