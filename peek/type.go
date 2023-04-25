package peek

import (
	"io"
	"net"
)

type toReader interface {
	ReaderReplaceable() bool
	ToReader() io.Reader
}

type toWriter interface {
	WriterReplaceable() bool
	ToWriter() io.Writer
}

type Conn interface {
	net.Conn
	Peeker
	toReader
	toWriter
}

type Peeker interface {
	Peek(n int) ([]byte, error)
}

func ToReader(reader io.Reader) io.Reader {
	if reader, ok := reader.(toReader); ok {
		if reader.ReaderReplaceable() {
			return ToReader(reader.ToReader())
		}
	}
	return reader
}

func ToWriter(writer io.Writer) io.Writer {
	if writer, ok := writer.(toWriter); ok {
		if writer.WriterReplaceable() {
			return ToWriter(writer.ToWriter())
		}
	}
	return writer
}

type ReadCached interface {
	ReadCached() []byte
}
