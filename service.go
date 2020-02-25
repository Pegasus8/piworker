package main

import (
	"fmt"
	"syscall"
	"os"

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
		log.Fatal(err)
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
		log.Fatal(err)
	}

	switch action {
	case "install":
		{
			err = pwService.Install()
			if err != nil {
				log.Fatalln("Error when trying to install the service:", err.Error())
			}
			log.Println("Service installed correctly!")
		}
	case "delete":
		{
			err = pwService.Uninstall()
			if err != nil {
				log.Fatalln("Error when trying to delete the service:", err.Error())
			}
			log.Println("Service deleted correctly!")
		}
	case "start":
		{
			err = pwService.Start()
			if err != nil {
				log.Fatalln("Error when trying to start the service:", err.Error())
			}
			log.Println("Service started successfully!")
		}
	case "stop":
		{
			err := pwService.Stop()
			if err != nil {
				log.Fatalln("Error when trying to stop the service:", err.Error())
			}
			log.Println("Service stopped successfully!")
		}
	case "status":
		{
			status, err := pwService.Status()
			if err != nil {
				log.Fatalln("Error when trying to get the status of the service:", err.Error())
			}
			switch status {
			case service.StatusRunning:
				log.Println("Service status: Running")
			case service.StatusStopped:
				log.Println("Service status: Stopped")
			case service.StatusUnknown:
				// From kardianos/service documentation.
				log.Printf("Service status: Unknown\nStatus is unable to be determined due to an error or it was not installed.\n")

			}
		}
	default:
		{
			log.Printf("Unrecognized action '%s'\n", action)
			os.Exit(1)
		}
	}
}
