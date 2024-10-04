package ss2022

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/wwqgtxx/wstunnel/fallback/ssaead"

	"gitlab.com/go-extension/aes-ccm"
	"lukechampine.com/blake3"
)

const (
	// RequestHeaderFixedChunkLength
	// https://github.com/Shadowsocks-NET/shadowsocks-specs/blob/main/2022-1-shadowsocks-2022-edition.md#312-format
	// Request stream:
	// +--------+------------------------+---------------------------+------------------------+---------------------------+---+
	// |  salt  | encrypted header chunk |  encrypted header chunk   | encrypted length chunk |  encrypted payload chunk  |...|
	// +--------+------------------------+---------------------------+------------------------+---------------------------+---+
	// | 16/32B |     11B + 16B tag      | variable length + 16B tag |  2B length + 16B tag   | variable length + 16B tag |...|
	// +--------+------------------------+---------------------------+------------------------+---------------------------+---+
	//
	RequestHeaderFixedChunkLength = 1 + 8 + 2

	// PacketMinimalHeaderSize
	// https://github.com/Shadowsocks-NET/shadowsocks-specs/blob/main/2022-1-shadowsocks-2022-edition.md#322-format-and-separate-header
	// Packet:
	// +---------------------------+---------------------------+
	// | encrypted separate header |       encrypted body      |
	// +---------------------------+---------------------------+
	// |            16B            | variable length + 16B tag |
	// +---------------------------+---------------------------+
	//
	// Separate header:
	// +------------+-----------+
	// | session ID | packet ID |
	// +------------+-----------+
	// |     8B     |   u64be   |
	// +------------+-----------+
	//
	PacketMinimalHeaderSize = 16 + 16
)

var (
	ErrMissingPSK = errors.New("missing psk")
)

func NewMethod(method string, password string) (*Method, error) {
	m := &Method{
		name: method,
	}
	switch method {
	case "2022-blake3-aes-128-gcm":
		m.keySaltLength = 16
		m.constructor = aeadCipher(aes.NewCipher, cipher.NewGCM)
		m.blockConstructor = aes.NewCipher
	case "2022-blake3-aes-256-gcm":
		m.keySaltLength = 32
		m.constructor = aeadCipher(aes.NewCipher, cipher.NewGCM)
		m.blockConstructor = aes.NewCipher
	case "2022-blake3-aes-128-ccm":
		m.keySaltLength = 16
		m.constructor = aeadCipher(aes.NewCipher, func(cipher cipher.Block) (cipher.AEAD, error) { return ccm.NewCCM(cipher) })
		m.blockConstructor = aes.NewCipher
	case "2022-blake3-aes-256-ccm":
		m.keySaltLength = 32
		m.constructor = aeadCipher(aes.NewCipher, func(cipher cipher.Block) (cipher.AEAD, error) { return ccm.NewCCM(cipher) })
		m.blockConstructor = aes.NewCipher
	default:
		return nil, fmt.Errorf("unsupported method: %s", method)
	}

	if password == "" {
		return nil, ErrMissingPSK
	}
	keyStrList := strings.Split(password, ":")
	pskList := make([][]byte, len(keyStrList))
	for i, keyStr := range keyStrList {
		kb, err := base64.StdEncoding.DecodeString(keyStr)
		if err != nil {
			return nil, fmt.Errorf("decode psk: %w", err)
		}
		pskList[i] = kb
	}

	psk := pskList[0]

	if len(psk) != m.keySaltLength {
		if len(psk) < m.keySaltLength {
			return nil, ssaead.ErrBadKey
		} else if len(psk) > m.keySaltLength {
			psk = Key(psk, m.keySaltLength)
		} else {
			return nil, ErrMissingPSK
		}
	}

	for _, key := range pskList[1:] {
		if len(key) < m.keySaltLength {
			return nil, ssaead.ErrBadKey
		} else if len(key) > m.keySaltLength {
			key = Key(key, m.keySaltLength)
		}

		var hash [aes.BlockSize]byte
		hash512 := blake3.Sum512(key)
		copy(hash[:], hash512[:])
		m.uPSKHash = append(m.uPSKHash, hash)
		m.uPSK = append(m.uPSK, key)
		uCipher, err := m.blockConstructor(key)
		if err != nil {
			return nil, err
		}
		m.uCipher = append(m.uCipher, uCipher)
	}

	var err error
	m.udpBlockCipher, err = aes.NewCipher(psk)
	if err != nil {
		return nil, err
	}

	m.psk = psk
	return m, nil
}

func Key(key []byte, keyLength int) []byte {
	psk := sha256.Sum256(key)
	return psk[:keyLength]
}

func SessionKey(psk []byte, salt []byte, keyLength int) []byte {
	sessionKey := make([]byte, len(psk)+len(salt))
	copy(sessionKey, psk)
	copy(sessionKey[len(psk):], salt)
	outKey := make([]byte, keyLength)
	blake3.DeriveKey(outKey, "shadowsocks 2022 session subkey", sessionKey)
	return outKey
}

var aeadCipher = ssaead.AeadCipher

type Method struct {
	name             string
	keySaltLength    int
	constructor      func(key []byte) (cipher.AEAD, error)
	blockConstructor func(key []byte) (cipher.Block, error)
	udpBlockCipher   cipher.Block
	psk              []byte
	uPSK             [][]byte
	uPSKHash         [][aes.BlockSize]byte
	uCipher          []cipher.Block
}
