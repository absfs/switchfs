//go:build darwin

package switchfs

import (
	"strconv"
	"strings"
	"syscall"
)

// hostMajorVersion returns the macOS major version (26 for macOS 26), or 0
// when it can't be read.
func hostMajorVersion() int {
	v, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.SplitN(v, ".", 2)[0])
	return n
}
