package ssaead

import (
	"github.com/wwqgtxx/wstunnel/peek"

	"golang.org/x/exp/slices"
)

type Pair[T any] struct {
	Name   string
	Method *Method
	Val    T
}

type Tester[T any] struct {
	Lists []Pair[T]
}

func NewTester[T any]() *Tester[T] {
	return &Tester[T]{}
}

func (t *Tester[T]) Add(name, method, password string, val T) (err error) {
	pair := Pair[T]{Name: name, Val: val}
	pair.Method, err = NewMethod(method, nil, password)
	if err != nil {
		return
	}
	t.Lists = append(t.Lists, pair)
	slices.SortFunc(t.Lists, func(a, b Pair[T]) bool {
		return a.Method.keySaltLength < b.Method.keySaltLength
	})
	return
}

func (t *Tester[T]) Test(peeker peek.Peeker, cb func(name string, val T)) (bool, error) {
	var err error
	var lastPeekBuf []byte
	var lastPeekLen int
	for _, pair := range t.Lists {
		name, method, val := pair.Name, pair.Method, pair.Val
		peekLen := method.keySaltLength + PacketLengthBufferSize + Overhead
		if lastPeekLen != peekLen {
			lastPeekLen = peekLen
			lastPeekBuf, err = peeker.Peek(peekLen)
			if err != nil {
				return false, err
			}
		}
		header := lastPeekBuf
		key := make([]byte, method.keySaltLength)
		_, err = Kdf(method.key, header[:method.keySaltLength], key)
		if err != nil {
			return false, err
		}
		readCipher, err := method.constructor(key)
		if err != nil {
			return false, err
		}
		lengthChunk := header[method.keySaltLength:]
		length := make([]byte, PacketLengthBufferSize)
		_, err = readCipher.Open(length[:0], peek.Zero[:readCipher.NonceSize()], lengthChunk, nil)
		if err != nil {
			continue
		}
		cb(name, val)
		return true, nil
	}
	return false, nil
}

func (t *Tester[T]) TestPacket(packet []byte) (bool, string, T) {
	var err error
	var emptyVal T
	for _, pair := range t.Lists {
		name, method, val := pair.Name, pair.Method, pair.Val
		if len(packet) <= method.keySaltLength+Overhead {
			continue
		}
		key := make([]byte, method.keySaltLength)
		_, err = Kdf(method.key, packet[:method.keySaltLength], key)
		if err != nil {
			return false, "", emptyVal
		}
		readCipher, err := method.constructor(key)
		if err != nil {
			return false, "", emptyVal
		}
		_, err = readCipher.Open(
			make([]byte, 0, len(packet)-Overhead),
			peek.Zero[:readCipher.NonceSize()],
			packet[method.keySaltLength:],
			nil,
		)
		if err != nil {
			continue
		}
		return true, name, val
	}
	return false, "", emptyVal
}
