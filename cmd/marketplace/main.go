package main

import (
	"log"
	"net/http"
	"os"

	"example.com/marketplace-handoff/marketplace"
)

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	server := &marketplace.Server{Infrai: &marketplace.InfraiClient{APIKey: key}, Store: marketplace.NewStore()}
	log.Println("marketplace listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", server.Handler()))
}
