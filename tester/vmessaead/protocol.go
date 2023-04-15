package vmessaead

import (
	"crypto/md5"

	"github.com/gofrs/uuid/v5"
)

const (
	CipherOverhead = 16
)

const (
	KDFSaltConstAuthIDEncryptionKey             = "AES Auth ID Encryption"
	KDFSaltConstAEADRespHeaderLenKey            = "AEAD Resp Header Len Key"
	KDFSaltConstAEADRespHeaderLenIV             = "AEAD Resp Header Len IV"
	KDFSaltConstAEADRespHeaderPayloadKey        = "AEAD Resp Header Key"
	KDFSaltConstAEADRespHeaderPayloadIV         = "AEAD Resp Header IV"
	KDFSaltConstVMessAEADKDF                    = "VMess AEAD KDF"
	KDFSaltConstVMessHeaderPayloadAEADKey       = "VMess Header AEAD Key"
	KDFSaltConstVMessHeaderPayloadAEADIV        = "VMess Header AEAD Nonce"
	KDFSaltConstVMessHeaderPayloadLengthAEADKey = "VMess Header AEAD Key_Length"
	KDFSaltConstVMessHeaderPayloadLengthAEADIV  = "VMess Header AEAD Nonce_Length"
)

func Key(user uuid.UUID) (key [16]byte, err error) {
	md5hash := md5.New()
	_, err = md5hash.Write(user[:])
	if err != nil {
		return
	}
	_, err = md5hash.Write([]byte("c48619fe-8f02-49e0-b9e9-edf763e17e21"))
	if err != nil {
		return
	}
	md5hash.Sum(key[:0])
	return
}
