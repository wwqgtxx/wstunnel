package tunnel

import (
	"io"
	"log"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

//go:linkname modws2_32 golang.org/x/sys/windows.modws2_32
var modws2_32 *windows.LazyDLL

var procrecv = modws2_32.NewProc("recv")

//go:linkname errnoErr golang.org/x/sys/windows.errnoErr
func errnoErr(e syscall.Errno) error

func recv(s windows.Handle, buf []byte, flags int32) (n int32, err error) {
	var _p0 *byte
	if len(buf) > 0 {
		_p0 = &buf[0]
	}
	r0, _, e1 := syscall.SyscallN(procrecv.Addr(), uintptr(s), uintptr(unsafe.Pointer(_p0)), uintptr(len(buf)), uintptr(flags))
	n = int32(r0)
	if n == -1 {
		err = errnoErr(e1)
	}
	return
}

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
		hasData := false
		err = srcRaw.Read(func(fd uintptr) (done bool) {
			if !hasData {
				hasData = true
				// golang's internal/poll.FD.RawRead will Use a zero-byte read as a way to get notified when this
				// socket is readable if we return false. So the `recv` syscall will not block the system thread.
				return false
			}
			n, err := recv(windows.Handle(fd), getBuf(), 0)
			rn = int(n)
			if n <= 0 {
				putBuf()
			}
			switch {
			case n == 0 && err == nil:
				sysErr = io.EOF
			case err == windows.WSAEWOULDBLOCK || err == syscall.EAGAIN || err == syscall.EWOULDBLOCK || err == syscall.EINTR:
				return false
				//sysErr = nil
			default:
				sysErr = err
			}
			hasData = false
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
