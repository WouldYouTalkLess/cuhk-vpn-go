//go:build darwin || linux

package vpn

import "cuhk-vpn-go/internal/config"

func prepare(s config.Settings, dir string) ([]string, []string, func() error, error) {
	return nil, nil, func() error { return nil }, nil
}
