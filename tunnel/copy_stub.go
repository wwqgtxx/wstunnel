//go:build !unix && !windows

package tunnel

import (
	"io"
	"syscall"
)

func syscallCopy(src io.Reader, srcRaw syscall.RawConn, dst io.Writer) (handed bool, written int64, err error) {
	return
}
