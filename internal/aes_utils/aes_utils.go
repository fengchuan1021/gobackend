package aes_utils

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

type Aes_request struct {
	Data string `json:"data"`
}

const keyStr = "sheep67890128977"

// deriveKey 使用 SHA-256 从字符串生成 32 字节密钥（与 C++ 端一致）
func deriveKey() []byte {
	h := sha256.Sum256([]byte(keyStr))
	return h[:]
}

// Encrypt 与 C++ aes_encrypt 兼容：AES-256-CBC，密钥为 SHA256(keyStr)，IV 为 16 字节随机，输出 Base64(IV||密文)
func Encrypt(plaintext string) (string, error) {
	key := deriveKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}

	mode := cipher.NewCBCEncrypter(block, iv)
	plain := []byte(plaintext)
	padLen := aes.BlockSize - len(plain)%aes.BlockSize
	plain = append(plain, bytes.Repeat([]byte{byte(padLen)}, padLen)...)

	ciphertext := make([]byte, len(plain))
	mode.CryptBlocks(ciphertext, plain)

	out := make([]byte, 0, len(iv)+len(ciphertext))
	out = append(out, iv...)
	out = append(out, ciphertext...)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt 解密由 C++ aes_encrypt 或本包 Encrypt 产生的数据：Base64 解码后前 16 字节为 IV，其余为密文
func Decrypt(base64Ciphertext string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(base64Ciphertext)
	if err != nil {
		return "", err
	}
	if len(raw) < aes.BlockSize {
		return "", errors.New("ciphertext too short")
	}

	key := deriveKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	iv := raw[:aes.BlockSize]
	ciphertext := raw[aes.BlockSize:]
	if len(ciphertext)%aes.BlockSize != 0 {
		return "", errors.New("ciphertext length not multiple of block size")
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	plain := make([]byte, len(ciphertext))
	mode.CryptBlocks(plain, ciphertext)

	// PKCS#7 去填充
	padLen := int(plain[len(plain)-1])
	if padLen <= 0 || padLen > aes.BlockSize {
		return "", errors.New("invalid padding")
	}
	for i := 0; i < padLen; i++ {
		if plain[len(plain)-1-i] != byte(padLen) {
			return "", errors.New("invalid padding")
		}
	}
	plain = plain[:len(plain)-padLen]
	return string(plain), nil
}
