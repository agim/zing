// Package vault seals private application data with the installation master key.
package vault

import "github.com/agim/lidza/pkg/credentials"

func Seal(value string) (string, error) {
	key, e := credentials.Key(".")
	if e != nil {
		return "", e
	}
	return credentials.Encrypt(key, []byte(value))
}
func Open(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	key, e := credentials.Key(".")
	if e != nil {
		return "", e
	}
	v, e := credentials.Decrypt(key, value)
	return string(v), e
}
