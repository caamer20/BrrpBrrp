//go:build !windows

package installer

import (
	"context"
	"errors"
)

func nativeTrustCertificate(context.Context, []byte) error {
	return errors.New("Download the certificate and trust it using this device's certificate settings.")
}
