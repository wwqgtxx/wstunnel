package ss2022

import (
	"crypto/aes"
	"crypto/subtle"
	"slices"

	"github.com/wwqgtxx/wstunnel/fallback/ssaead"
	"github.com/wwqgtxx/wstunnel/peek"

	"lukechampine.com/blake3"
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
	pair.Method, err = NewMethod(method, password)
	if err != nil {
		return
	}
	t.Lists = append(t.Lists, pair)
	slices.SortFunc(t.Lists, func(a, b Pair[T]) int {
		return (a.Method.keySaltLength + aes.BlockSize*len(a.Method.uPSKHash)) -
			(b.Method.keySaltLength + aes.BlockSize*len(b.Method.uPSKHash))
	})
	return
}

func (t *Tester[T]) Test(peeker peek.Peeker, cb func(name string, val T)) (bool, error) {
	var err error
	var lastPeekBuf []byte
	var lastPeekLen int
ListsLoop:
	for _, pair := range t.Lists {
		name, method, val := pair.Name, pair.Method, pair.Val
		peekLen := method.keySaltLength + aes.BlockSize*len(method.uPSKHash) + RequestHeaderFixedChunkLength + ssaead.Overhead
		if lastPeekLen != peekLen {
			lastPeekLen = peekLen
			lastPeekBuf, err = peeker.Peek(peekLen)
			if err != nil {
				return false, err
			}
		}
		header := lastPeekBuf

		requestSalt := header[:method.keySaltLength]

		psk := method.psk
		for i, uPSKHash := range method.uPSKHash {
			var _eiHeader [aes.BlockSize]byte
			eiHeader := _eiHeader[:]
			copy(eiHeader, header[method.keySaltLength+aes.BlockSize*i:method.keySaltLength+aes.BlockSize*(i+1)])

			keyMaterial := make([]byte, method.keySaltLength*2)
			copy(keyMaterial, psk)
			copy(keyMaterial[method.keySaltLength:], requestSalt)
			identitySubkey := make([]byte, method.keySaltLength)
			blake3.DeriveKey(identitySubkey, "shadowsocks 2022 identity subkey", keyMaterial)
			b, err := method.blockConstructor(identitySubkey)
			if err != nil {
				continue ListsLoop
			}
			b.Decrypt(eiHeader, eiHeader)

			if _eiHeader == uPSKHash {
				psk = method.uPSK[i]
			} else {
				continue ListsLoop
			}
		}

		requestKey := SessionKey(psk, requestSalt, method.keySaltLength)
		readCipher, err := method.constructor(requestKey)
		if err != nil {
			continue
		}

		fixedLengthHeaderChunk := header[method.keySaltLength+aes.BlockSize*len(method.uPSKHash):]
		fixedLengthHeader := make([]byte, RequestHeaderFixedChunkLength)
		_, err = readCipher.Open(fixedLengthHeader[:0], peek.Zero[:readCipher.NonceSize()], fixedLengthHeaderChunk, nil)
		if err != nil {
			continue
		}
		cb(name, val)
		return true, nil
	}
	return false, nil
}

func (t *Tester[T]) TestPacket(packet []byte) (bool, string, T) {
	var emptyVal T
ListsLoop:
	for _, pair := range t.Lists {
		name, method, val := pair.Name, pair.Method, pair.Val
		if len(packet) <= PacketMinimalHeaderSize+aes.BlockSize*len(method.uPSKHash) {
			continue
		}
		packetHeader := make([]byte, aes.BlockSize)
		method.udpBlockCipher.Decrypt(packetHeader, packet[:aes.BlockSize])

		psk := method.psk
		uCipher := method.udpBlockCipher
		for i, uPSKHash := range method.uPSKHash {
			var _eiHeader [aes.BlockSize]byte
			eiHeader := _eiHeader[:]
			uCipher.Decrypt(eiHeader, packet[aes.BlockSize+aes.BlockSize*i:aes.BlockSize+aes.BlockSize*(i+1)])
			subtle.XORBytes(eiHeader, eiHeader, packetHeader)

			if _eiHeader == uPSKHash {
				psk = method.uPSK[i]
				uCipher = method.uCipher[i]
			} else {
				continue ListsLoop
			}
		}

		key := SessionKey(psk, packetHeader[:8], method.keySaltLength)
		readCipher, err := method.constructor(key)
		if err != nil {
			continue
		}

		_, err = readCipher.Open(
			make([]byte, 0, len(packet)-aes.BlockSize-aes.BlockSize*len(method.uPSKHash)-ssaead.Overhead),
			packetHeader[4:16],
			packet[16+aes.BlockSize*len(method.uPSKHash):],
			nil,
		)
		if err != nil {
			continue
		}
		return true, name, val
	}
	return false, "", emptyVal
}
