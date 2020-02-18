package main

import (
	"flag"
	"fmt"
	"github.com/Pegasus8/piworker/core/configs"
	"os"
)

func handleFlags() {
	err := initConfigs()
	if err != nil {
		fmt.Println("Error when reading the configs:", err)
		os.Exit(1)
	}
	newUserFlag := flag.Bool("new-user", false, "create a new user")
	username := flag.String("username", "", "the name of the new user")
	password := flag.String("password", "", "the password of the new user")
	admin := flag.Bool("admin", false, "if the user will be admin")

	changeUserPasswordFlag := flag.Bool("change-password", false, "change the password of an existent user")
	// Uses the flag "username" too
	newPassword := flag.String("new-password", "", "the new password to the user")

	serviceFlag := flag.Bool("service", false, "manage PiWorker's service")

	flag.Parse()

	if *newUserFlag && *changeUserPasswordFlag && *serviceFlag {
		fmt.Println("You can't use the flags 'new-user', 'change-password' and 'service' at the same time.")
		os.Exit(1)
	}

	if *serviceFlag {
		if len(os.Args) != 3 {
			fmt.Printf("It seems that you are using more/less arguments than expected.\n" +
				"Flags to manage the service:\n" +
				"\t--service install -> To install the service\n" +
				"\t--service delete -> To delete the service (if already installed)\n" +
				"\t--service start -> To start the service (if already installed)\n" +
				"\t--service stop -> To stop the service (if is active)\n",
			)
			os.Exit(1)
		}
		serviceFlagHandler(os.Args[2])
	}

	if *newUserFlag {
		newUserFlagHandler(*username, *password, *admin)
	}

	if *changeUserPasswordFlag {
		changeUserPasswordFlagHandler(*username, *newPassword)
	}
}

func newUserFlagHandler(username, password string, admin bool) {
	if username == "" || password == "" {
		fmt.Println("Some of the flags used to create a new user are empty (username and/or password) which is not allowed.")
		os.Exit(1)
	}
	err := configs.NewUser(username, password, admin)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	} else {
		fmt.Println("New user created correctly")
	}
}

func changeUserPasswordFlagHandler(username, newPassword string) {
	if username == "" || newPassword == "" {
		fmt.Println("Some of the flags used to change the password of a user are empty (username and/or new password) which is not allowed.")
		os.Exit(1)
	}
	err := configs.ChangeUserPassword(username, newPassword)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	} else {
		fmt.Printf("Password of the user '%s' changed correctly!\n", username)
	}

}

func serviceFlagHandler(action string) {
	fmt.Println("Action to perform over PiWorker service:", action)
	manageService(action)
}
