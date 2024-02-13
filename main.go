package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/nbd-wtf/go-nostr"
)

func StringEnv(key string) string {
	value, ok := os.LookupEnv(key)
	if !ok {
		log.Fatalf("address env variable \"%s\" not set, usual", key)
	}
	return value
}

var (
	IXIAN_SK = StringEnv("IXIAN_SK")
)

func main() {

	log.Println("Starting...")

	ctx := context.Background()

	// Elasticsearch store relay
	relay, err := nostr.RelayConnect(ctx, "ws://localhost:3334")
	if err != nil {
		panic(err)
	}

	// Slice store relay
	cache, err := nostr.RelayConnect(ctx, "ws://localhost:3335")
	if err != nil {
		panic(err)
	}

	h := Handler{
		relay: relay,
		cache: cache,
	}

	r := mux.NewRouter()

	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))
	r.PathPrefix("/fonts/").Handler(http.StripPrefix("/fonts/", http.FileServer(http.Dir("./fonts"))))

	r.HandleFunc("/", h.Home).Methods("GET")
	r.HandleFunc("/validate", h.Validate).Methods("GET")
	r.HandleFunc("/pull", h.Articles).Methods("GET")
	r.HandleFunc("/search", h.Search).Methods("GET")
	r.HandleFunc("/{naddr:[a-zA-Z0-9]+}", h.Article).Methods("GET")
	//r.HandleFunc("/{id:[a-zA-Z0-9]+}", h.Highlight).Methods("GET")

	s := &http.Server{
		Addr:    "127.0.0.1:8080",
		Handler: r,
	}

	// Create a channel to listen for OS signals
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		err := s.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	<-stop

	// Create a context with a timeout for the server's shutdown process
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = h.Close()
	if err != nil {
		log.Fatalf("closing subscriptions failed:%+v", err)
	}

	err = s.Shutdown(ctx)
	if err != nil {
		log.Fatalf("server shutdown failed:%+v", err)
	}

	log.Println("Server gracefully stopped")
}
