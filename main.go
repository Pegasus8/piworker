package main

import (
	"log"
	"github.com/Pegasus8/piworker/webui"
)

func main() {
	log.Println("Running PiWorker")

	webui.Run() // Run on another goroutine
}