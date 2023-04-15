package vmessaead

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"hash/crc32"

	"github.com/wwqgtxx/wstunnel/common"
	"github.com/wwqgtxx/wstunnel/peek"

	"github.com/gofrs/uuid/v5"
)

type Pair struct {
	Name       string
	Block      cipher.Block
	ClientImpl common.ClientImpl
}

type Tester struct {
	Lists []Pair
}

func NewTester() *Tester {
	return &Tester{}
}

const (
	AuthIdSize = 16
)

func (t *Tester) Add(name, userId string, clientImpl common.ClientImpl) (err error) {
	pair := Pair{Name: name, ClientImpl: clientImpl}
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

func (t *Tester) Test(peeker peek.Peeker, cb func(Name string, clientImpl common.ClientImpl)) (bool, error) {
	header, err := peeker.Peek(AuthIdSize)
	if err != nil {
		return false, err
	}

	authId := header[:AuthIdSize]
	var decodedId [AuthIdSize]byte
	for _, pair := range t.Lists {
		name, userIdBlock, clientImpl := pair.Name, pair.Block, pair.ClientImpl
		userIdBlock.Decrypt(decodedId[:], authId)
		checksum := binary.BigEndian.Uint32(decodedId[12:])
		if crc32.ChecksumIEEE(decodedId[:12]) != checksum {
			continue
		}
		cb(name, clientImpl)
		return true, nil
	}
	return false, nil
}
