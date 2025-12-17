package main

import (
	"context"
	"log"

	borego "github.com/FrontMage/bore-go"
)

func main() {
	client, err := borego.NewClient("localhost", 8000, "0.0.0.0", 0, "")
	if err != nil {
		log.Fatalf("failed to start client: %v", err)
	}
	log.Printf("public port assigned: %d", client.RemotePort())

	ctx := context.Background()
	if err := client.Listen(ctx); err != nil {
		log.Fatalf("listen exited: %v", err)
	}
}
