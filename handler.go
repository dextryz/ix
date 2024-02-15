package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"text/template"

	"github.com/dextryz/nip23"
	"github.com/dextryz/nip84"
	nos "github.com/dextryz/nostr"
	"github.com/dextryz/pipe"

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
	lastUpdated map[string]nostr.Timestamp
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

func (s *Handler) Articles(w http.ResponseWriter, r *http.Request) {

	ctx := context.Background()

	npub := r.URL.Query().Get("npub")

	log.Printf("pulling articles for %s", npub)

	articles := pipe.New(s.cfg.Relays).Articles([]string{npub}, s.db.lastUpdated[npub]).Query().WithIdentifier()

	aa := articles.Events()

	// update timestamp
	if len(aa) > 0 {
		// Get all the NIP-84 events of the set of NIP-23 articles
		// FIXME has to be pipeline from FromStore()
		highlights := articles.Highlights().Query()
		for _, h := range highlights.Events() {
			err := s.db.SaveEvent(ctx, h)
			if err != nil {
				log.Fatalln(err)
			}
		}

		for _, e := range aa {

			err := s.db.SaveEvent(ctx, e)
			if err != nil {
				log.Fatalln(err)
			}

            if e.CreatedAt > s.db.lastUpdated[npub] {
                s.db.lastUpdated[npub] = e.CreatedAt + 1
            }
		}
	}

	// Storage

	_, pk, err := nip19.Decode(npub)
	if err != nil {
		panic(err)
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
	fmt.Println(len(ch))

	notes := []*nip23.Article{}
	for e := range ch {
		a, err := nip23.ToArticle(e)
		if err != nil {
			log.Fatalln(err)
		}
		notes = append(notes, a)
	}

	//s.store(npub)
	//notes := s.search("")

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
