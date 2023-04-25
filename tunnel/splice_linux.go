package tunnel

import (
	"fmt"
	"io"
	"log"
	"syscall"

	"golang.org/x/sys/unix"
)

const maxSpliceSize = 1 << 20

func splice(src io.Reader, srcRaw syscall.RawConn, dst io.Writer, srcDst syscall.RawConn) (handed bool, n int64, err error) {
	log.Printf("splice %T %T", src, dst)
	handed = true
	var pipeFDs [2]int
	err = unix.Pipe2(pipeFDs[:], syscall.O_CLOEXEC|syscall.O_NONBLOCK)
	if err != nil {
		return
	}
	defer unix.Close(pipeFDs[0])
	defer unix.Close(pipeFDs[1])

	_, _ = unix.FcntlInt(uintptr(pipeFDs[0]), unix.F_SETPIPE_SZ, maxSpliceSize)
	var readN int
	var readErr error
	var writeSize int
	var writeErr error
	readFunc := func(fd uintptr) (done bool) {
		p0, p1 := unix.Splice(int(fd), nil, pipeFDs[1], nil, maxSpliceSize, unix.SPLICE_F_NONBLOCK)
		readN = int(p0)
		readErr = p1
		return readErr != unix.EAGAIN
	}
	writeFunc := func(fd uintptr) (done bool) {
		for writeSize > 0 {
			p0, p1 := unix.Splice(pipeFDs[0], nil, int(fd), nil, writeSize, unix.SPLICE_F_NONBLOCK|unix.SPLICE_F_MOVE)
			writeN := int(p0)
			writeErr = p1
			if writeErr != nil {
				return writeErr != unix.EAGAIN
			}
			writeSize -= writeN
		}
		return true
	}
	for {
		err = srcRaw.Read(readFunc)
		if err != nil {
			readErr = err
		}
		if readErr != nil {
			if readErr == unix.EINVAL || readErr == unix.ENOSYS {
				handed = false
				return
			}
			err = fmt.Errorf("splice read: %w", readErr)
			return
		}
		if readN == 0 {
			return
		}
		writeSize = readN
		err = srcDst.Write(writeFunc)
		if err != nil {
			writeErr = err
		}
		if writeErr != nil {
			err = fmt.Errorf("splice write: %w", writeErr)
			return
		}
	}
}
