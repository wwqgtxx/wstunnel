package tunnel

import (
	"io"
	"log"
)

func stdCopy(dst io.Writer, src io.Reader) (written int64, err error) {
	log.Printf("stdCopy %T %T", src, dst)
	buf := BufPool.Get().([]byte)
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw < 0 || nr < nw {
				nw = 0
			}
			written += int64(nw)
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}
	BufPool.Put(buf)
	return
}
