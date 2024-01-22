package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"text/template"
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

type Article struct {
	Id          string // NIP-19 (note1...)
	Image       string
	Title       string
    Npub string
	HashTags    []string // #focus #think without to the # in sstring
	MdContent   string
	HtmlContent string
	PublishedAt string
}

type Handler struct {
	Store eventstore.Store
	subs  []*nostr.Subscription
}

func (s *Handler) Close() error {
	for _, sub := range s.subs {
		sub.Close()
	}
	s.Store.Close()
	return nil
}

func (s *Handler) Home(w http.ResponseWriter, r *http.Request) {

	tmpl, err := template.ParseFiles("static/index.html", "static/card.html")
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

func (s *Handler) Events(w http.ResponseWriter, r *http.Request) {

	log.Println("pull profile NIP-01")

	npub := r.URL.Query().Get("search")

	ctx := context.Background()
	relay, err := nostr.RelayConnect(ctx, "wss://nostr-01.yakihonne.com")
	if err != nil {
		panic(err)
	}

	var filters nostr.Filters
	if _, v, err := nip19.Decode(npub); err == nil {
		pub := v.(string)
		filters = []nostr.Filter{{
			Kinds:   []int{nostr.KindArticle},
			Authors: []string{pub},
			Limit:   1000,
		}}
	} else {
		panic(err)
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	sub, err := relay.Subscribe(ctx, filters)
	if err != nil {
		panic(err)
	}

	notes := []*Article{}
	for ev := range sub.Events {
		// channel will stay open until the ctx is cancelled (in this case, context timeout)
        a, err := eventToArticle(ev)
        if (err != nil) {
            log.Fatalln(err)
        }
		notes = append(notes, a)
	}

	tmpl, err := template.ParseFiles("static/card.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tmpl.Execute(w, notes)
}

func (s *Handler) Article(w http.ResponseWriter, r *http.Request) {

	log.Println("to be impl")

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
        Npub: npub,
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
