package main

import (
	"flag"
	"fmt"
	"os"
	"github.com/Pegasus8/piworker/processment/configs"
)

func handleFlags() {
	newUserFlag := flag.Bool("new-user", false, "create a new user")
	username := flag.String("username", "", "the name of the new user")
	password := flag.String("password", "", "the password of the new user")
	admin := flag.Bool("admin", false, "if the user will be admin")

	flag.Parse()

	if *newUserFlag {
		newUserFlagHandler(*username, *password, *admin)
	}
}

func newUserFlagHandler(username, password string, admin bool) {
	if username == "" || password == "" {
		fmt.Println("Some of the flags used to create a new user are empty (username and/or password) which is not allowed.")
		os.Exit(1)
	}
	err := configs.NewUser(username, password, admin)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	} else {
		fmt.Println("New user created correctly")
	}
}