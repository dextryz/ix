package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"text/template"

	"github.com/dextryz/nip23"
	"github.com/dextryz/nip84"
	nos "github.com/dextryz/nostr"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

var ErrNotFound = errors.New("todo list not found")

type Handler struct {
	cfg   *nos.Config
	relay *nostr.Relay
	cache *nostr.Relay
}

func (s *Handler) Close() error {
	s.relay.Close()
	return nil
}

func (s *Handler) Home(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("static/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = tmpl.ExecuteTemplate(w, "index.html", "")
	if err != nil {
		fmt.Println("Error executing template:", err)
	}
}

func (s *Handler) Articles(w http.ResponseWriter, r *http.Request) {

	npub := r.URL.Query().Get("npub")

	log.Printf("pulling articles for %s", npub)

	// Last cached was 10 mins ago
	ctx := context.Background()
	since := nostr.Now() - 3600
	list, err := s.cache.QuerySync(ctx, nostr.Filter{Since: &since, Limit: 100})
	if err != nil {
		panic(err)
	}

	// If no events was pulled and cached in the last 10 mins.
	if len(list) == 0 {
		s.store(npub)
	}

	notes := s.search("")

	tmpl, err := template.ParseFiles("static/home.html", "static/card.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tmpl.Execute(w, notes)
}

func (s *Handler) Search(w http.ResponseWriter, r *http.Request) {
	notes := s.search(r.URL.Query().Get("keywords"))
	tmpl, err := template.ParseFiles("static/card.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, notes)
}

func (s *Handler) store(npub string) {

	log.Printf("Caching articles for %s", npub)

	ctx := context.Background()

	for _, relay := range s.cfg.Relays {

		r, err := nostr.RelayConnect(ctx, relay)
		if err != nil {
			panic(err)
		}

		_, v, err := nip19.Decode(npub)
		if err != nil {
			panic(err)
		}

		var filter nostr.Filter
		pub := v.(string)
		filter = nostr.Filter{
			Kinds:   []int{nostr.KindArticle},
			Authors: []string{pub},
			Limit:   1000,
		}

		log.Println("A")

		events, err := r.QuerySync(ctx, filter)
		if err != nil {
			log.Fatalln(err)
		}

		log.Println("B")

		ids := []string{}
		for _, e := range events {
			ids = append(ids, e.ID)
		}

		f := nostr.Filter{
			IDs:     ids,
			Authors: []string{pub},
			Limit:   1, // There should only be one article with this ID.
		}

		log.Println("C")

		cached, err := s.relay.QuerySync(ctx, f)
		if err != nil {
			log.Fatalln(err)
		}
		cmap := map[string]interface{}{}
		for _, v := range cached {
			cmap[v.ID] = struct{}{}
		}

		log.Println("D")

		_, sk, err := nip19.Decode(s.cfg.Nsec)
		if err != nil {
			panic(err)
		}

		p := nostr.Event{CreatedAt: nostr.Now()}
		p.Sign(sk.(string))

		// Store the time when event was cached
		err = s.cache.Publish(ctx, p)
		if err != nil {
			log.Fatalln(err)
		}

		var wg sync.WaitGroup
		for _, e := range events {

			_, ok := cmap[e.ID]
			if !ok {
				wg.Add(1) // Be certain to Add before launching the goroutine!
				go func(ev *nostr.Event) {
					defer wg.Done()

					err = s.relay.Publish(ctx, *ev)
					if err != nil {
						log.Fatalln(err)
					}
				}(e)
			}

		}
		wg.Wait()

		log.Println("E")
	}

	log.Println("DONE")
}

func (s *Handler) search(keywords string) []*nip23.Article {

	log.Printf("searching for keywords: [%s]", keywords)

	filter := nostr.Filter{
		Kinds:  []int{nostr.KindArticle},
		Search: keywords,
		Limit:  1000,
	}

	events, err := s.relay.QuerySync(context.Background(), filter)
	if err != nil {
		log.Fatalln(err)
	}

	log.Printf("article found: %d with keywords: %s", len(events), keywords)

	notes := []*nip23.Article{}
	for _, e := range events {
		a, err := nip23.ToArticle(e)
		if err != nil {
			log.Fatalln(err)
		}
		notes = append(notes, a)
	}

	return notes
}

func (s *Handler) Article(w http.ResponseWriter, r *http.Request) {

	log.Println("retrieving article from cache")

	naddr := r.PathValue("naddr")

	a, err := nip23.RequestArticle(s.cfg, naddr)
	if err != nil {
		log.Fatalln(err)
	}

	a, err = MdToHtml(a)
	if err != nil {
		log.Fatalln(err)
	}

	highlights, err := nip84.RequestHighlights(s.cfg, naddr)
	if err != nil {
		log.Fatalln(err)
	}

	for _, h := range highlights {
		a, err = ReplaceHighlight(h, a)
		if err != nil {
			log.Fatalln(err)
		}
	}

	tmpl, err := template.ParseFiles("static/article.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tmpl.Execute(w, a)
}

func (s *Handler) Validate(w http.ResponseWriter, r *http.Request) {

	pk := r.URL.Query().Get("search")

	if pk != "" {

		prefix, _, err := nip19.Decode(pk)

		if err != nil {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`<span class="message error">Invalid entity</span>`))
			return
		}

		if prefix != "npub" {
			log.Println("start with npub")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`<span class="message error">Start with npub</span>`))
			return
		}

		// Add text to show valid if you want to.
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<span class="message success"> </span>`))
	}
}
