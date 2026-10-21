//go:build !windows

package secret

import "errors"

func protectDPAPI([]byte) (string, error) {
	return "", errors.New("DPAPI unavailable")
}

func unprotectDPAPI(string) ([]byte, error) {
	return nil, errors.New("DPAPI unavailable")
}
