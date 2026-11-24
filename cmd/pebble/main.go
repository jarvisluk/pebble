package main

import (
	"log"
	"os"

	"bot-hub/internal/cli"
)

func main() {
	log.SetFlags(log.Ltime | log.Lmsgprefix)
	cli.Main(os.Args[1:])
}
