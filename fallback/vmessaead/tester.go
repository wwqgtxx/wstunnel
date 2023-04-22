package vmessaead

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"hash/crc32"

	"github.com/wwqgtxx/wstunnel/peek"

	"github.com/gofrs/uuid/v5"
)

type Pair[T any] struct {
	Name  string
	Block cipher.Block
	Val   T
}

type Tester[T any] struct {
	Lists []Pair[T]
}

func NewTester[T any]() *Tester[T] {
	return &Tester[T]{}
}

const (
	AuthIdSize = 16
)

func (t *Tester[T]) Add(name, userId string, val T) (err error) {
	pair := Pair[T]{Name: name, Val: val}
	userUUID := uuid.FromStringOrNil(userId)
	if userUUID == uuid.Nil {
		userUUID = uuid.NewV5(userUUID, userId)
	}
	userCmdKey, err := Key(userUUID)
	if err != nil {
		return
	}
	pair.Block, err = aes.NewCipher(KDF(userCmdKey[:], KDFSaltConstAuthIDEncryptionKey)[:AuthIdSize])
	if err != nil {
		return
	}

	t.Lists = append(t.Lists, pair)
	return
}

func (t *Tester[T]) Test(peeker peek.Peeker, cb func(name string, val T)) (bool, error) {
	header, err := peeker.Peek(AuthIdSize)
	if err != nil {
		return false, err
	}

	authId := header[:AuthIdSize]
	var decodedId [AuthIdSize]byte
	for _, pair := range t.Lists {
		name, userIdBlock, val := pair.Name, pair.Block, pair.Val
		userIdBlock.Decrypt(decodedId[:], authId)
		checksum := binary.BigEndian.Uint32(decodedId[12:])
		if crc32.ChecksumIEEE(decodedId[:12]) != checksum {
			continue
		}
		cb(name, val)
		return true, nil
	}
	return false, nil
}
