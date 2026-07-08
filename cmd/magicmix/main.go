package main

import (
	"log"

	"github.com/YakDriver/magicmix/internal/cli"
)

func main() {
	if err := cli.Run(); err != nil {
		log.Fatal(err)
	}
}
