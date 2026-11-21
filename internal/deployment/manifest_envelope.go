package deployment

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

const manifestKeyService = "com.ideploy.deployment.manifest-key"
const manifestKeyAccount = "release-manifests-v1"

var manifestKeyMu sync.Mutex

// The local database holds only an AES-GCM envelope. The key stays in Keychain.
func localManifestKey(ctx context.Context) ([]byte, error) {
	if runtime.GOOS != "darwin" {
		return nil, errors.New("发布快照加密需要 macOS 钥匙串")
	}
	manifestKeyMu.Lock()
	defer manifestKeyMu.Unlock()
	read := func() ([]byte, error) {
		output, err := exec.CommandContext(ctx, "security", "find-generic-password", "-a", manifestKeyAccount, "-s", manifestKeyService, "-w").Output()
		if err != nil {
			return nil, err
		}
		key, err := hex.DecodeString(strings.TrimSpace(string(output)))
		if err != nil || len(key) != 32 {
			return nil, errors.New("发布快照加密密钥无效")
		}
		return key, nil
	}
	if key, err := read(); err == nil {
		return key, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if _, err := exec.CommandContext(ctx, "security", "add-generic-password", "-a", manifestKeyAccount, "-s", manifestKeyService, "-w", hex.EncodeToString(key)).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("无法创建发布快照加密密钥: %w", err)
	}
	return read()
}

func sealManifest(ctx context.Context, manifest string) (string, error) {
	if manifest == "" {
		return "", nil
	}
	key, err := localManifestKey(ctx)
	if err != nil {
		return "", err
	}
	return sealManifestWithKey(key, manifest)
}

func sealManifestWithKey(key []byte, manifest string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(append(nonce, aead.Seal(nil, nonce, []byte(manifest), nil)...)), nil
}

func openManifest(ctx context.Context, envelope string) (string, error) {
	if envelope == "" {
		return "", nil
	}
	key, err := localManifestKey(ctx)
	if err != nil {
		return "", err
	}
	return openManifestWithKey(key, envelope)
}

func openManifestWithKey(key []byte, envelope string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(envelope)
	if err != nil {
		return "", errors.New("发布快照密文无效")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(data) < aead.NonceSize() {
		return "", errors.New("发布快照密文无效")
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], nil)
	if err != nil {
		return "", errors.New("发布快照无法解密，请检查本机钥匙串")
	}
	return string(plain), nil
}

func OpenManifestEnvelope(ctx context.Context, envelope string) (string, error) {
	return openManifest(ctx, envelope)
}
