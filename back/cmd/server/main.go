package main

import (
	"os"

	"test/internal/app"
)

func main() {
	app.Main(os.Args[1:])
}
