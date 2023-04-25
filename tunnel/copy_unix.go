//go:build unix

package tunnel

import (
	"io"
	"log"
	"syscall"
)

func syscallCopy(src io.Reader, srcRaw syscall.RawConn, dst io.Writer) (handed bool, written int64, err error) {
	log.Printf("syscallCopy %T %T", src, dst)
	handed = true
	var sysErr error = nil
	var buf []byte
	getBuf := func() []byte {
		if buf == nil {
			buf = BufPool.Get().([]byte)
		}
		return buf
	}
	putBuf := func() {
		if buf != nil {
			BufPool.Put(buf)
			buf = nil
		}
	}
	for {
		var rn int
		var wn int
		err = srcRaw.Read(func(fd uintptr) (done bool) {
			n, err := syscall.Read(int(fd), getBuf())
			rn = n
			if n <= 0 {
				putBuf()
			}
			switch {
			case n == 0 && err == nil:
				sysErr = io.EOF
			case err == syscall.EAGAIN || err == syscall.EWOULDBLOCK || err == syscall.EINTR:
				return false
				//sysErr = nil
			default:
				sysErr = err
			}
			return true
		})
		if err == nil {
			err = sysErr
		}
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return
		}
		wn, err = dst.Write(buf[:rn])
		putBuf()
		written += int64(wn)
		if rn != wn {
			err = io.ErrShortWrite
			return
		}
		if err != nil {
			return
		}
	}
}
