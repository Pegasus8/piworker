package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/Pegasus8/piworker/core/signals"

	"github.com/kardianos/service"
)

type program struct{}

func (p *program) Start(s service.Service) error {
	go p.run()
	return nil
}

func (p *program) run() {
	start()
}

func (p *program) Stop(s service.Service) error {
	if signals.Shutdown != nil {
		signals.Shutdown <- syscall.SIGINT
	}
	return nil
}

func manageService(action string) {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	serviceConfigs := &service.Config{
		Name:             "PiWorker",
		DisplayName:      "PiWorker",
		Description:      "This service executes PiWorker.",
		WorkingDirectory: wd,
	}
	p := &program{}
	pwService, err := service.New(p, serviceConfigs)
	if err != nil {
		fmt.Println("Error when trying to create a new instance of Service:", err.Error())
		os.Exit(1)
	}

	switch action {
	case "install":
		{
			err = pwService.Install()
			if err != nil {
				fmt.Println("Error when trying to install the service:", err.Error())
				os.Exit(1)
			}
			fmt.Println("Service installed correctly!")
		}
	case "delete":
		{
			err = pwService.Uninstall()
			if err != nil {
				fmt.Println("Error when trying to delete the service:", err.Error())
				os.Exit(1)
			}
			fmt.Println("Service deleted correctly!")
		}
	case "start":
		{
			err = pwService.Start()
			if err != nil {
				fmt.Println("Error when trying to start the service:", err.Error())
				os.Exit(1)
			}
			fmt.Println("Service started successfully!")
		}
	case "stop":
		{
			err := pwService.Stop()
			if err != nil {
				fmt.Println("Error when trying to stop the service:", err.Error())
				os.Exit(1)
			}
			fmt.Println("Service stopped successfully!")
		}
	case "status":
		{
			status, err := pwService.Status()
			if err != nil {
				fmt.Println("Error when trying to get the status of the service:", err.Error())
				os.Exit(1)
			}
			switch status {
			case service.StatusRunning:
				fmt.Println("Service status: Running")
			case service.StatusStopped:
				fmt.Println("Service status: Stopped")
			case service.StatusUnknown:
				// From kardianos/service documentation.
				fmt.Printf("Service status: Unknown\nStatus is unable to be determined due to an error or because it was not installed.\n")
			}
		}
	default:
		{
			fmt.Printf("Unrecognized action '%s'\n", action)
			os.Exit(1)
		}
	}
}
