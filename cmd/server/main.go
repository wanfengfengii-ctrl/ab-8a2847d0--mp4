package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"fmp4audit/internal/server"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           server.New(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	log.Printf("fmp4audit listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
