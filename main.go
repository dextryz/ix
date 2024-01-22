package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fiatjaf/eventstore/sqlite3"
	"github.com/gorilla/mux"
)

func main() {

	log.Println("Starting...")

	db := &sqlite3.SQLite3Backend{
		DatabaseURL:       "nostr_sqlite.db",
		QueryLimit:        1_000_000,
		QueryAuthorsLimit: 1_000_000,
		QueryKindsLimit:   1_000_000,
		QueryIDsLimit:     1_000_000,
		QueryTagsLimit:    1_000_000,
	}

	db.Init()

	h := Handler{
		Store: db,
	}

	r := mux.NewRouter()

	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))
	r.PathPrefix("/fonts/").Handler(http.StripPrefix("/fonts/", http.FileServer(http.Dir("./fonts"))))

	r.HandleFunc("/", h.Home).Methods("GET")
	r.HandleFunc("/validate", h.Validate).Methods("GET")
	r.HandleFunc("/events", h.Events).Methods("GET")
	r.HandleFunc("/{id:[a-zA-Z0-9]+}", h.Article).Methods("GET")

	s := &http.Server{
		Addr:    "127.0.0.1:8081",
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

	err := h.Close()
	if err != nil {
		log.Fatalf("closing subscriptions failed:%+v", err)
	}

	err = s.Shutdown(ctx)
	if err != nil {
		log.Fatalf("server shutdown failed:%+v", err)
	}

	log.Println("Server gracefully stopped")
}
