package main

import "github.com/kasyap/rag-ingestor/app"

func main() {
	application := app.New()
	application.RegisterRoutes()
	application.Run()
}
