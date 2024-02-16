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

	"github.com/fiatjaf/eventstore"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

var ErrNotFound = errors.New("todo list not found")

type Tag struct {
	value      string
	identifier string
}

type EventStore struct {
	eventstore.Store
	UpdatedAt map[string]nostr.Timestamp
}

type Handler struct {
	cfg *nos.Config
	db  *EventStore
}

func (s *Handler) Close() error {
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

func (s *Handler) queryRelays(ctx context.Context, filter nostr.Filter) (ev []*nostr.Event) {

	var m sync.Map
	var wg sync.WaitGroup
	for _, url := range s.cfg.Relays {

		wg.Add(1)
		go func(wg *sync.WaitGroup, url string) {
			defer wg.Done()

			r, err := nostr.RelayConnect(ctx, url)
			if err != nil {
				panic(err)
			}

			events, err := r.QuerySync(ctx, filter)
			if err != nil {
				log.Fatalln(err)
			}

			for _, e := range events {
				m.Store(e.ID, e)
			}

		}(&wg, url)
	}
	wg.Wait()

	m.Range(func(_, v any) bool {
		ev = append(ev, v.(*nostr.Event))
		return true
	})

	return ev
}

func (s *Handler) queryArticles(ctx context.Context, npub string) (ev []*nostr.Event, tags []string, err error) {

	_, pk, err := nip19.Decode(npub)
	if err != nil {
		panic(err)
	}

	// Only pull the latest events
	ts := s.db.UpdatedAt[npub]

	f := nostr.Filter{
		Kinds:   []int{nostr.KindArticle},
		Authors: []string{pk.(string)},
		Since:   &ts,
		Limit:   500,
	}

	// For each article with an identifier, create
	// a 'd' tag to be used to requesting highlights
	for _, e := range s.queryRelays(ctx, f) {

		identifier := ""
		for _, t := range e.Tags {
			if t.Key() == "d" {
				identifier = t.Value()
			}
		}

		if identifier != "" {
			ev = append(ev, e)
			tag := fmt.Sprintf("%d:%s:%s", e.Kind, e.PubKey, identifier)
			tags = append(tags, tag)
		}
	}

	return ev, tags, nil
}

func (s *Handler) queryHighlights(ctx context.Context, npub string, tags []string) []*nostr.Event {

	if len(tags) > 0 {
		f := nostr.Filter{
			Kinds: []int{nos.KindHighlight},
			Tags: nostr.TagMap{
				"a": tags,
			},
		}
		return s.queryRelays(ctx, f)
	}

	return nil
}

func (s *Handler) Articles(w http.ResponseWriter, r *http.Request) {

	ctx := context.Background()

	npub := r.URL.Query().Get("npub")

	log.Printf("pulling articles for %s", npub)

	// 1. Pull and store Articles and Highlights.

	events, tags, err := s.queryArticles(ctx, npub)
	if err != nil {
		panic(err)
	}

	for _, e := range events {

		err := s.db.SaveEvent(ctx, e)
		if err != nil {
			log.Fatalln(err)
		}

		if e.CreatedAt > s.db.UpdatedAt[npub] {
			s.db.UpdatedAt[npub] = e.CreatedAt + 1
		}
	}

	for _, h := range s.queryHighlights(ctx, npub, tags) {
		err := s.db.SaveEvent(ctx, h)
		if err != nil {
			log.Fatalln(err)
		}
	}

	// 2. Retrieve and convert events from local cache.

	_, pk, err := nip19.Decode(npub)
	if err != nil {
		log.Fatalln(err)
	}

	filter := nostr.Filter{
		Kinds:   []int{nostr.KindArticle},
		Authors: []string{pk.(string)},
		Limit:   500,
	}

	ch, err := s.db.QueryEvents(ctx, filter)
	if err != nil {
		log.Fatalln(err)
	}

	notes := []*nip23.Article{}
	for e := range ch {
		a, err := nip23.ToArticle(e)
		if err != nil {
			log.Fatalln(err)
		}
		notes = append(notes, a)
	}

	// 3. Generate HTML and send to webclient writer.

	tmpl, err := template.ParseFiles("static/home.html", "static/card.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tmpl.Execute(w, notes)
}

func (s *Handler) Article(w http.ResponseWriter, r *http.Request) {

	log.Println("retrieving article from cache")

	ctx := context.Background()

	prefix, data, err := nip19.Decode(r.PathValue("naddr"))
	if err != nil {
		log.Fatalln(err)
	}
	if prefix != "naddr" {
		log.Fatalln(err)
	}
	ep := data.(nostr.EntityPointer)

	filter := nostr.Filter{
		Kinds: []int{nostr.KindArticle},
		Tags: nostr.TagMap{
			"d": []string{ep.Identifier},
		},
		Limit: 1,
	}

	ch, err := s.db.QueryEvents(ctx, filter)
	if err != nil {
		log.Fatalln(err)
	}

	a := &nip23.Article{}
	for e := range ch {
		a, err = nip23.ToArticle(e)
		if err != nil {
			log.Fatalln(err)
		}
	}

	a, err = MdToHtml(a)
	if err != nil {
		log.Fatalln(err)
	}

	// Apply highlights

	tag := fmt.Sprintf("%d:%s:%s", ep.Kind, ep.PublicKey, ep.Identifier)
	filter = nostr.Filter{
		Kinds: []int{nos.KindHighlight},
		Tags: nostr.TagMap{
			"a": []string{tag},
		},
		Limit: 1,
	}

	ch, err = s.db.QueryEvents(ctx, filter)
	if err != nil {
		log.Fatalln(err)
	}

	var highlights []*nip84.Highlight
	for e := range ch {
		a, err := nip84.ToHighlight(e)
		if err != nil {
			log.Fatalln(err)
		}
		highlights = append(highlights, &a)
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
