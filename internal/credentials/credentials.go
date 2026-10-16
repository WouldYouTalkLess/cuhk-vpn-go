// Package credentials supports OS credentials and an explicitly portable vault.
package credentials

import (
	"cuhk-vpn-go/internal/config"
	"errors"
	"github.com/zalando/go-keyring"
)

const service = "CuhkVpnGo"

var ErrNotFound = keyring.ErrNotFound

func local() (FileStore, error) {
	d, err := config.Directory()
	return FileStore{Directory: d}, err
}

func Get(mode, account string) (string, error) {
	switch mode {
	case config.PasswordSystem:
		return keyring.Get(service, account)
	case config.PasswordPortable:
		s, err := local()
		if err != nil {
			return "", err
		}
		return s.Get(account)
	default:
		return "", errors.New("无效的密码保存方式")
	}
}

func Set(mode, account, password string) error {
	switch mode {
	case config.PasswordSystem:
		return keyring.Set(service, account, password)
	case config.PasswordPortable:
		s, err := local()
		if err != nil {
			return err
		}
		return s.Set(account, password)
	default:
		return errors.New("无效的密码保存方式")
	}
}

func Delete(mode, account string) error {
	switch mode {
	case config.PasswordSystem:
		return keyring.Delete(service, account)
	case config.PasswordPortable:
		s, err := local()
		if err != nil {
			return err
		}
		return s.Delete(account)
	default:
		return errors.New("无效的密码保存方式")
	}
}
