//go:build !linux

package tunnel

import (
	"io"
	"syscall"
)

func splice(src io.Reader, srcRaw syscall.RawConn, dst io.Writer, srcDst syscall.RawConn) (handed bool, n int64, err error) {
	return
}
