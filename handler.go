package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"text/template"
	"time"

	"github.com/gorilla/mux"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

type Article struct {
	Id          string // NIP-19 (note1...)
	Image       string
	Title       string
	Npub        string
	HashTags    []string // #focus #think without to the # in sstring
	MdContent   string
	HtmlContent string
	PublishedAt string
}

type Handler struct {
	relay *nostr.Relay
}

func (s *Handler) Close() error {
	s.relay.Close()
	return nil
}

func (s *Handler) Home(w http.ResponseWriter, r *http.Request) {

	tmpl, err := template.ParseFiles("static/index.html", "static/home.html", "static/card.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	notes := []*nostr.Event{}
	err = tmpl.ExecuteTemplate(w, "index.html", notes)
	if err != nil {
		fmt.Println("Error executing template:", err)
	}
}

func (s *Handler) Articles(w http.ResponseWriter, r *http.Request) {

	npub := r.URL.Query().Get("npub")

    log.Printf("pulling articles for %s", npub)

	s.cache(npub)

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

func (s *Handler) cache(npub string) {

	log.Printf("Caching articles for %s", npub)

	ctx := context.Background()

	for _, relay := range []string{"wss://nostr-01.yakihonne.com", "wss://relay.damus.io/"} {

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

		var wg sync.WaitGroup
		for _, e := range events {

			_, ok := cmap[e.ID]
			if !ok {
				wg.Add(1) // Be certain to Add before launching the goroutine!
				go func(ev *nostr.Event) {
					defer wg.Done()
					err := s.relay.Publish(ctx, *ev)
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

func (s *Handler) search(keywords string) []*Article {

	log.Printf("Searching for keywords: [%s]", keywords)

	filter := nostr.Filter{
		Kinds:  []int{nostr.KindArticle},
		Search: keywords,
		Limit:  1000,
	}

	events, err := s.relay.QuerySync(context.Background(), filter)
	if err != nil {
		log.Fatalln(err)
	}

	log.Printf("Article found: %d with keywords: %s", len(events), keywords)

	notes := []*Article{}
	for _, e := range events {
		a, err := eventToArticle(e)
		if err != nil {
			log.Fatalln(err)
		}
		notes = append(notes, a)
	}

	return notes
}

func (s *Handler) Article(w http.ResponseWriter, r *http.Request) {

	log.Println("retrieving article from cache")

	vars := mux.Vars(r)
	nid := vars["id"]

	// Convert NIP-19 nevent123... to NIP-01 hex ID
	_, v, err := nip19.Decode(nid)
	if err != nil {
		log.Fatalln(err)
	}

	npub := "npub14ge829c4pvgx24c35qts3sv82wc2xwcmgng93tzp6d52k9de2xgqq0y4jk"
	_, pk, err := nip19.Decode(npub)
	if err != nil {
		panic(err)
	}

	log.Print(nid)

	filter := nostr.Filter{
		IDs:     []string{v.(string)},
		Authors: []string{pk.(string)},
		Limit:   5, // There should only be one article with this ID.
	}

	ctx := context.Background()
	events, err := s.relay.QuerySync(ctx, filter)
	if err != nil {
		log.Fatalln(err)
	}

	log.Print(events)

	articles := []*Article{}
	for _, e := range events {
		a, err := eventToArticle(e)
		if err != nil {
			log.Fatalln(err)
		}
		articles = append(articles, a)
	}

	tmpl, err := template.ParseFiles("static/article.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tmpl.Execute(w, articles[0])
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

		if prefix[0] != 'n' {
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

func eventToArticle(e *nostr.Event) (*Article, error) {

	// Sample Unix timestamp: 1635619200 (represents 2021-10-30)
	unixTimestamp := int64(e.CreatedAt)

	// Convert Unix timestamp to time.Time
	t := time.Unix(unixTimestamp, 0)

	// Format time.Time to "yyyy-mm-dd"
	createdAt := t.Format("2006-01-02")

	// Encode NIP-01 event id to NIP-19 note id
	id, err := nip19.EncodeNote(e.ID)
	if err != nil {
		return nil, err
	}

	// Encode NIP-01 pubkey to NIP-19 npub
	npub, err := nip19.EncodePublicKey(e.PubKey)
	if err != nil {
		return nil, err
	}

	a := &Article{
		Id:          id,
		Npub:        npub,
		MdContent:   e.Content,
		HtmlContent: mdToHtml(e.Content),
		PublishedAt: createdAt,
	}

	for _, t := range e.Tags {
		if t.Key() == "image" {
			a.Image = t.Value()
		}
		if t.Key() == "title" {
			a.Title = t.Value()
		}
		// TODO: Check the # prefix and filter in tags.
		if t.Key() == "t" {
			a.HashTags = append(a.HashTags, t.Value())
		}
	}

	return a, nil
}
