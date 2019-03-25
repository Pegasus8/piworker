package data

import (
	"os"
	"log"
)

func init() {
	// Create data path if not exists
	err := os.MkdirAll(DataPath, os.ModePerm)
	if err != nil {
		log.Fatalln(err)
	}
}