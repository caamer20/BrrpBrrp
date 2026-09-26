package main

import (
	"brrpbrrp/internal/desktop"
	"brrpbrrp/internal/installer"
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
	"os"
	"path/filepath"
	"strings"
)

const serviceName = "BrrpBrrp"

func runPlatform(ctx context.Context) error {
	service, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if service {
		return svc.Run(serviceName, &windowsService{})
	}
	if len(os.Args) > 1 {
		command := os.Args[1]
		if command == "tray" {
			err := desktop.Run(ctx)
			if err != nil {
				desktop.Alert(err.Error())
			}
			return err
		}
		if command == "service-start" || command == "service-stop" || command == "service-restart" || command == "service-stop-and-exit" {
			if len(os.Args) != 2 {
				return errors.New("service controls do not accept arguments")
			}
			if launched, err := installer.Elevate(); launched || err != nil {
				return err
			}
			err := controlService(ctx, strings.TrimSuffix(command, "-and-exit"))
			if err != nil {
				desktop.Alert(err.Error())
			} else if command == "service-stop-and-exit" {
				desktop.CloseTray()
			}
			return err
		}
	}
	return run(ctx, os.Args[1:])
}

type windowsService struct{}

func (*windowsService) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, os.Args[1:]) }()
	current := svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	status <- current
	for {
		select {
		case err := <-done:
			if err != nil {
				return true, 1
			}
			return false, 0
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				status <- current
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, WaitHint: 20000}
				cancel()
				if err := <-done; err != nil {
					return true, 1
				}
				return false, 0
			}
		}
	}
}

func manageService(command, config string) error {
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	if command == "service-uninstall" {
		service, err := manager.OpenService(serviceName)
		if err != nil {
			return err
		}
		defer service.Close()
		state, err := service.Query()
		if err != nil {
			return err
		}
		if state.State != svc.Stopped {
			return errors.New("stop BrrpBrrp before uninstalling its service")
		}
		return service.Delete()
	}
	if config == "" {
		return errors.New("service-install requires --config with an absolute settings file path")
	}
	config, err = filepath.Abs(config)
	if err != nil {
		return err
	}
	if _, err = os.Stat(config); err != nil {
		return err
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	service, err := manager.CreateService(serviceName, binary, mgr.Config{DisplayName: "BrrpBrrp", Description: "Private community chat and voice", StartType: mgr.StartAutomatic, DelayedAutoStart: true, ServiceStartName: `NT AUTHORITY\LocalService`, SidType: windows.SERVICE_SID_TYPE_UNRESTRICTED}, "serve", "--config", config)
	if err != nil {
		return err
	}
	defer service.Close()
	fmt.Println("Service installed. Grant LocalService access to the data folder, then start BrrpBrrp.")
	return nil
}
