package tunnel

import (
	"io"
	"log"
	"syscall"

	"golang.org/x/sys/windows"
)

//go:generate go run golang.org/x/sys/windows/mkwinsyscall -output zsyscall_windows.go copy_windows.go

//sys recv(h windows.Handle, buf []byte, flags int32) (n int32, err error) [failretval==-1] = ws2_32.recv

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
