package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"cuhk-vpn-go/internal/config"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// The key intentionally travels with the vault. Anyone with the entire data
// directory can decrypt it; this is portability, not machine-bound protection.
type FileStore struct{ Directory string }

const header = "CUHKVPN-PASSWORD\x01"

var fileMu sync.Mutex
var errInvalidVault = errors.New("本地密码文件或密钥损坏，请重新输入密码；请勿删除仍需使用的密钥")

func (s FileStore) directory() string { return filepath.Join(s.Directory, "passwords") }
func (s FileStore) path(account string) string {
	h := sha256.Sum256([]byte(account))
	return filepath.Join(s.directory(), hex.EncodeToString(h[:])+".bin")
}

func readRegular(path string, max int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > max {
		return nil, errInvalidVault
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err == nil && int64(len(b)) > max {
		return nil, errInvalidVault
	}
	return b, err
}

func (s FileStore) key(create bool) ([]byte, error) {
	path := filepath.Join(s.directory(), "key.bin")
	b, err := readRegular(path, 32)
	if err == nil {
		if len(b) != 32 {
			return nil, errInvalidVault
		}
		return b, nil
	}
	if !os.IsNotExist(err) || !create {
		return nil, err
	}
	if err = os.MkdirAll(s.directory(), 0700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.directory())
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".bin") {
			// Never silently generate a replacement key over an existing vault.
			return nil, errInvalidVault
		}
	}
	b = make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(err) {
		return s.key(false)
	}
	if err != nil {
		return nil, err
	}
	_, writeErr := f.Write(b)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if err = errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return b, nil
}

func aead(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithRandomNonce(b)
}

func (s FileStore) Get(account string) (string, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	b, err := readRegular(s.path(account), 64<<10)
	if os.IsNotExist(err) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(string(b), header) {
		return "", errInvalidVault
	}
	key, err := s.key(false)
	if err != nil {
		return "", errInvalidVault
	}
	defer clear(key)
	gcm, err := aead(key)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, nil, b[len(header):], []byte(header+account))
	if err != nil {
		return "", errInvalidVault
	}
	defer clear(plain)
	return string(plain), nil
}

func (s FileStore) Set(account, password string) error {
	fileMu.Lock()
	defer fileMu.Unlock()
	if account == "" || password == "" || len(password) > 16<<10 || strings.ContainsAny(password, "\x00\r\n") {
		return errors.New("请输入有效账号和密码")
	}
	key, err := s.key(true)
	if err != nil {
		return err
	}
	defer clear(key)
	gcm, err := aead(key)
	if err != nil {
		return err
	}
	b := gcm.Seal([]byte(header), nil, []byte(password), []byte(header+account))
	return config.WritePrivateFile(s.path(account), b)
}

func (s FileStore) Delete(account string) error {
	fileMu.Lock()
	defer fileMu.Unlock()
	err := os.Remove(s.path(account))
	if os.IsNotExist(err) {
		return ErrNotFound
	}
	// Keep the shared key: other accounts or backups may still need it.
	return err
}
