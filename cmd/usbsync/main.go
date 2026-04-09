package main

import (
	"log"

	"usbsync/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		showStartupError(err)
		log.Fatal(err)
	}
}
