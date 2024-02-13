package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/gorilla/mux"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

var ErrNotFound = errors.New("todo list not found")

type Article struct {
	Id          string // NIP-19 (naddr...)
	Image       string
	Title       string
	HashTags    []string // #focus #think without to the # in sstring
	MdContent   string
	HtmlContent string
	PublishedAt string
}

type Handler struct {
	relay *nostr.Relay
	cache *nostr.Relay
}

func (s *Handler) Close() error {
	s.relay.Close()
	return nil
}

var KindHighlight = 9802

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
	since := nostr.Now() - 10
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

		_, sk, err := nip19.Decode(IXIAN_SK)
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
		a, err := s.eventToArticle(e)
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
	nid := vars["naddr"]

	// Convert NIP-19 nevent123... to NIP-01 hex ID
	prefix, data, err := nip19.Decode(nid)
	if err != nil {
		log.Fatalln(err)
	}
	if prefix != "naddr" {
		log.Fatalln(err)
	}
	ep := data.(nostr.EntityPointer)
	if ep.Kind != nostr.KindArticle {
		log.Fatalln(err)
	}

	log.Printf("NADDR (Article): %s", nid)

	filter := nostr.Filter{
		Authors: []string{ep.PublicKey},
		Kinds:   []int{ep.Kind},
		Tags: nostr.TagMap{
			"d": []string{ep.Identifier},
			//"a": []string{fmt.Sprintf("%d:%s:%s", ep.Kind, ep.PublicKey, ep.Identifier)},
		},
		Limit: 10, // There should only be one article with this ID.
	}

	fmt.Println(filter)

	// 	ctx := context.Background()
	// 	events, err := s.relay.QuerySync(ctx, filter)
	// 	if err != nil {
	// 		log.Fatalln(err)
	// 	}
	ctx := context.Background()
	pool := nostr.NewSimplePool(ctx)
	events := pool.QuerySingle(ctx, []string{"wss://relay.damus.io/", "wss://relay.highlighter.com/", "wss://nostr-01.yakihonne.com"}, filter)

	log.Print(events)

	articles := []*Article{}
	a, err := s.eventToArticle(events.Event)
	if err != nil {
		log.Fatalln(err)
	}
	articles = append(articles, a)

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

func (s *Handler) eventToArticle(e *nostr.Event) (*Article, error) {

	// Encode NIP-01 event id to NIP-19 note id
	var identifier string
	for _, tag := range e.Tags {
		if tag.Key() == "d" {
			identifier = tag.Value()
		}
	}

	naddr, err := nip19.EncodeEntity(e.PubKey, e.Kind, identifier, []string{})
	if err != nil {
		return nil, err
	}

	fmt.Printf("\nEVENT: %v\n", e)
	fmt.Printf("\nIdentifier: %s\n", identifier)
	fmt.Printf("\nNAddr: %s\n", naddr)

	// TODO: Add highlisth div here
	content := mdToHtml(e.Content)

	// 1. Find all kind 9802 event that are linked to the article ID.
	tag := fmt.Sprintf("%d:%s:%s", e.Kind, e.PubKey, identifier)
	filter := nostr.Filter{
		Authors: []string{e.PubKey},
		Kinds:   []int{KindHighlight},
		Tags: nostr.TagMap{
			"a": []string{tag},
		},
	}

	// FIXME
	ctx := context.Background()
	pool := nostr.NewSimplePool(ctx)
	event := pool.QuerySingle(ctx, []string{"wss://relay.damus.io/", "wss://relay.highlighter.com/"}, filter)
	// 	if events == nil {
	// 		return nil, ErrNotFound
	// 	}

	log.Println("\nEVENTS")
	log.Println(event)

	//substring := "The primitives of value generation is the effective management of resources."
	substring := "looked"

	// 2. Replace the event content with a span and CSS class
	if strings.Contains(content, substring) {
		log.Println("SUBSTRING found")
		content = strings.ReplaceAll(content, substring, fmt.Sprintf("<span class='highlight'>%s</span>", substring))
	}

	// Sample Unix timestamp: 1635619200 (represents 2021-10-30)
	unixTimestamp := int64(e.CreatedAt)
	// Convert Unix timestamp to time.Time
	t := time.Unix(unixTimestamp, 0)
	// Format time.Time to "yyyy-mm-dd"
	createdAt := t.Format("2006-01-02")

	a := &Article{
		Id:          naddr,
		MdContent:   e.Content,
		HtmlContent: content,
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
