package installer

import (
	"context"
	"errors"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
	"time"
	"unsafe"
)

func nativeStop(ctx context.Context, p Plan) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	service, err := m.OpenService("BrrpBrrp")
	if err != nil {
		return err
	}
	defer service.Close()
	status, err := service.Query()
	if err != nil {
		return err
	}
	if status.State != svc.Stopped && status.State != svc.StopPending {
		if _, err := service.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	for status.State != svc.Stopped {
		select {
		case <-ctx.Done():
			return errors.New("The server did not stop in time. Check Windows Services and retry.")
		case <-time.After(200 * time.Millisecond):
		}
		status, err = service.Query()
		if err != nil {
			return err
		}
	}
	return nil
}

// An explicit wizard action trusts the displayed root for all users on this PC.
func nativeTrustCertificate(_ context.Context, der []byte) error {
	name, _ := windows.UTF16PtrFromString("ROOT")
	store, err := windows.CertOpenStore(windows.CERT_STORE_PROV_SYSTEM_W, 0, 0, windows.CERT_SYSTEM_STORE_LOCAL_MACHINE|windows.CERT_STORE_OPEN_EXISTING_FLAG, uintptr(unsafe.Pointer(name)))
	if err != nil {
		return err
	}
	defer windows.CertCloseStore(store, 0)
	cert, err := windows.CertCreateCertificateContext(windows.X509_ASN_ENCODING, &der[0], uint32(len(der)))
	if err != nil {
		return err
	}
	defer windows.CertFreeCertificateContext(cert)
	return windows.CertAddCertificateContextToStore(store, cert, windows.CERT_STORE_ADD_REPLACE_EXISTING, nil)
}
