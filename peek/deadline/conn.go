package deadline

// modify from https://github.com/SagerNet/sing/pull/32
// https://github.com/wwqgtxx/sing/blob/efbfb690948a4bb8b2faa8b8523ea8dab3f31cf3/common/bufio/deadline/reader.go

import (
	"io"
	"net"
	"os"
	"time"

	"github.com/wwqgtxx/wstunnel/atomic"
)

type readResult struct {
	buffer []byte
	err    error
}

type Conn struct {
	net.Conn
	deadline     atomic.TypedValue[time.Time]
	pipeDeadline pipeDeadline
	disablePipe  atomic.Bool
	inRead       atomic.Bool
	resultCh     chan *readResult
}

func New(conn net.Conn) *Conn {
	c := &Conn{
		Conn:         conn,
		pipeDeadline: makePipeDeadline(),
		resultCh:     make(chan *readResult, 1),
	}
	c.resultCh <- nil
	return c
}

func (c *Conn) Read(p []byte) (n int, err error) {
	select {
	case result := <-c.resultCh:
		if result != nil {
			n = copy(p, result.buffer)
			err = result.err
			if n >= len(result.buffer) {
				c.resultCh <- nil // finish cache read
			} else {
				result.buffer = result.buffer[n:]
				c.resultCh <- result // push back for next call
			}
			return
		} else {
			c.resultCh <- nil
			break
		}
	case <-c.pipeDeadline.wait():
		return 0, os.ErrDeadlineExceeded
	}

	if c.disablePipe.Load() {
		return c.Conn.Read(p)
	} else if c.deadline.Load().IsZero() {
		c.inRead.Store(true)
		defer c.inRead.Store(false)
		n, err = c.Conn.Read(p)
		return
	}

	<-c.resultCh
	go c.pipeRead(len(p))

	return c.Read(p)
}

func (c *Conn) pipeRead(size int) {
	buffer := make([]byte, size)
	n, err := c.Conn.Read(buffer)
	buffer = buffer[:n]
	c.resultCh <- &readResult{
		buffer: buffer,
		err:    err,
	}
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	if c.disablePipe.Load() {
		return c.Conn.SetReadDeadline(t)
	} else if c.inRead.Load() {
		c.disablePipe.Store(true)
		return c.Conn.SetReadDeadline(t)
	}
	c.deadline.Store(t)
	c.pipeDeadline.set(t)
	return nil
}

func (c *Conn) ReaderReplaceable() bool {
	select {
	case result := <-c.resultCh:
		c.resultCh <- result
		if result != nil {
			return false // cache reading
		} else {
			break
		}
	default:
		return false // pipe reading
	}
	return c.disablePipe.Load() || c.deadline.Load().IsZero()
}

func (c *Conn) ToReader() io.Reader {
	return c.Conn
}

func (c *Conn) WriterReplaceable() bool {
	return true
}

func (c *Conn) ToWriter() io.Writer {
	return c.Conn
}
