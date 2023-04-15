package ssaead

import (
	"github.com/wwqgtxx/wstunnel/common"
	"github.com/wwqgtxx/wstunnel/peek"

	"golang.org/x/exp/slices"
)

type Pair struct {
	Name       string
	Method     *Method
	ClientImpl common.ClientImpl
}

type Tester struct {
	Lists []Pair
}

func NewTester() *Tester {
	return &Tester{}
}

func (t *Tester) Add(name, method, password string, clientImpl common.ClientImpl) (err error) {
	pair := Pair{Name: name, ClientImpl: clientImpl}
	pair.Method, err = NewMethod(method, nil, password)
	if err != nil {
		return
	}
	t.Lists = append(t.Lists, pair)
	slices.SortFunc(t.Lists, func(a, b Pair) bool {
		return a.Method.keySaltLength < b.Method.keySaltLength
	})
	return
}

func (t *Tester) Test(peeker peek.Peeker, cb func(name string, clientImpl common.ClientImpl)) (bool, error) {
	for _, pair := range t.Lists {
		name, method, clientImpl := pair.Name, pair.Method, pair.ClientImpl
		header, err := peeker.Peek(method.keySaltLength + PacketLengthBufferSize + Overhead)
		if err != nil {
			return false, err
		}
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
		nonce := make([]byte, readCipher.NonceSize())
		length := make([]byte, PacketLengthBufferSize)
		_, err = readCipher.Open(length[:0], nonce, lengthChunk, nil)
		if err != nil {
			continue
		}
		cb(name, clientImpl)
		return true, nil
	}
	return false, nil
}
