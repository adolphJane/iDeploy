package sharedworkspace

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
)

const vaultVersion = 2

type Envelope struct {
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type VaultConfig struct {
	Version int    `json:"version"`
	Key     string `json:"key"`
}

func randomBytes(size int) ([]byte, error) {
	result := make([]byte, size)
	_, err := rand.Read(result)
	return result, err
}

func seal(key, plaintext []byte, context string) (Envelope, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return Envelope{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return Envelope{}, err
	}
	nonce, err := randomBytes(aead.NonceSize())
	if err != nil {
		return Envelope{}, err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, []byte(context))
	return Envelope{Nonce: base64.StdEncoding.EncodeToString(nonce), Ciphertext: base64.StdEncoding.EncodeToString(ciphertext)}, nil
}

func unseal(key []byte, envelope Envelope, context string) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("vault key is invalid")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("vault key is invalid")
	}
	nonce, nonceErr := base64.StdEncoding.DecodeString(envelope.Nonce)
	ciphertext, contentErr := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if nonceErr != nil || contentErr != nil || len(nonce) != aead.NonceSize() {
		return nil, errors.New("encrypted envelope is invalid")
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(context))
	if err != nil {
		return nil, errors.New("加密数据无效或密钥不匹配")
	}
	return plaintext, nil
}

func CreateVault() (VaultConfig, []byte, error) {
	key, err := randomBytes(32)
	if err != nil {
		return VaultConfig{}, nil, err
	}
	return VaultConfig{Version: vaultVersion, Key: base64.StdEncoding.EncodeToString(key)}, key, nil
}

func VaultKey(config VaultConfig) ([]byte, error) {
	if config.Version != vaultVersion {
		return nil, errors.New("不支持的共享保险库版本；请重置旧共享数据库")
	}
	key, err := base64.StdEncoding.DecodeString(config.Key)
	if err != nil || len(key) != 32 {
		return nil, errors.New("共享保险库密钥无效")
	}
	return key, nil
}

func EncryptSecret(key []byte, id, value string) (Envelope, error) {
	if id == "" {
		return Envelope{}, errors.New("secret id is required")
	}
	return seal(key, []byte(value), "ideploy-secret-v1:"+id)
}

func DecryptSecret(key []byte, id string, envelope Envelope) (string, error) {
	value, err := unseal(key, envelope, "ideploy-secret-v1:"+id)
	return string(value), err
}
