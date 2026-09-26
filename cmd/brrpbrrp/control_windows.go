package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func controlService(ctx context.Context, command string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	wait := func(want svc.State) error {
		for {
			state, err := s.Query()
			if err != nil {
				return err
			}
			if state.State == want {
				return nil
			}
			if want == svc.Running && state.State == svc.Stopped {
				return fmt.Errorf("BrrpBrrp did not start (Windows exit code %d, service code %d). Open setup to check the installation and server log.", state.Win32ExitCode, state.ServiceSpecificExitCode)
			}
			select {
			case <-ctx.Done():
				return errors.New("Windows did not finish the service operation within 40 seconds. Check Windows Services and the server log.")
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	if command != "service-start" {
		state, err := s.Query()
		if err != nil {
			return err
		}
		if state.State != svc.Stopped && state.State != svc.StopPending {
			if _, err = s.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
				return err
			}
		}
		if err := wait(svc.Stopped); err != nil {
			return err
		}
	}
	if command != "service-stop" {
		if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
			return err
		}
		return wait(svc.Running)
	}
	return nil
}
