package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
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

type Handler struct {
	wg        *sync.WaitGroup
	cfg       *nos.Config
	m         *sync.Map
	h         *sync.Map
	db        eventstore.Store
	timestamp map[string]nostr.Timestamp
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

	articles := pipe.New(s.cfg.Relays).Articles([]string{npub}, s.timestamp[npub]).Query().WithIdentifier().Sort()
	//articles.Stdout()

	aa := articles.Events()

	// update timestamp
	if len(aa) > 0 {
		s.timestamp[npub] = aa[len(aa)-1].CreatedAt + 60
	}

	for _, e := range aa {
		err := s.db.SaveEvent(ctx, e)
		if err != nil {
			log.Fatalln(err)
		}
	}

    // Storage

    _, pk, err := nip19.Decode(npub)
    if err != nil {
        panic(err)
    }

	filter := nostr.Filter{
		Kinds: []int{nostr.KindArticle},
        Authors: []string{pk.(string)},
        Limit: 500,
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


	//fmt.Printf("\n\n\nHIGHLIGHTS\n\n\n")

// 	// Get all the NIP-84 events of the set of NIP-23 articles
//     // FIXME has to be pipeline from FromStore()
// 	highlights := articles.Highlights().Query()
// 	//highlights.Stdout()
// 
// 	for _, h := range highlights.Events() {
// 		err := s.db.SaveEvent(ctx, h)
// 		if err != nil {
// 			log.Fatalln(err)
// 		}
// 	}
// 
// 	fmt.Println("moving on")

	//s.store(npub)
	//notes := s.search("")

	//fmt.Println(len(articles.Events()))

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

	for _, url := range s.cfg.Relays {

		s.wg.Add(1)
		go func(wg *sync.WaitGroup, url string) {
			defer wg.Done()

			r, err := nostr.RelayConnect(ctx, url)
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

			var tags []Tag
			var tt []string
			for _, e := range events {
				var identifier string
				for _, t := range e.Tags {
					if t.Key() == "d" {
						identifier = t.Value()
					}
				}
				s.m.Store(identifier, e)

				// Query hihglihgt here
				t := fmt.Sprintf("%d:%s:%s", nostr.KindArticle, pub, identifier)

				tag := Tag{
					value:      t,
					identifier: identifier,
				}

				tags = append(tags, tag)
				tt = append(tt, t)
			}

			f := nostr.Filter{
				Authors: []string{pub},
				Kinds:   []int{nos.KindHighlight},
				Tags: nostr.TagMap{
					"a": tt,
				},
			}
			ev, err := r.QuerySync(ctx, f)
			if err != nil {
				log.Println(err)
				return
			}
			for i, e := range ev {
				s.h.Store(tags[i].identifier, e)
			}

		}(s.wg, url)

		log.Println("E")
	}
	s.wg.Wait()

	log.Println("DONE")
}

func (s *Handler) search(keywords string) []*nip23.Article {

	log.Printf("searching for keywords: [%s]", keywords)

	keys := []string{}
	s.m.Range(func(k, v any) bool {
		keys = append(keys, k.(string))
		return true
	})
	sort.Slice(keys, func(i, j int) bool {
		lhs, ok := s.m.Load(keys[i])
		if !ok {
			return false
		}
		rhs, ok := s.m.Load(keys[j])
		if !ok {
			return false
		}
		return lhs.(*nostr.Event).CreatedAt.Time().Before(rhs.(*nostr.Event).CreatedAt.Time())
	})

	notes := []*nip23.Article{}
	for _, key := range keys {
		vv, ok := s.m.Load(key)
		if !ok {
			continue
		}
		a, err := nip23.ToArticle(vv.(*nostr.Event))
		if err != nil {
			log.Fatalln(err)
		}
		notes = append(notes, a)
	}

	return notes
}

func (s *Handler) Article(w http.ResponseWriter, r *http.Request) {

	log.Println("retrieving article from cache")

	ctx := context.Background()

	naddr := r.PathValue("naddr")

	// 	a, err := nip23.RequestArticle(s.cfg, naddr)
	// 	if err != nil {
	// 		log.Fatalln(err)
	// 	}

	prefix, data, err := nip19.Decode(naddr)
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

	// 	e, ok := s.m.Load(ep.Identifier)
	// 	if !ok {
	// 		log.Fatalln(fmt.Errorf("unable to load %s", naddr))
	// 	}

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

	// 	highlights, err := nip84.RequestHighlights(s.cfg, naddr)
	// 	if err != nil {
	// 		log.Fatalln(err)
	// 	}

	// 	keys := []string{}
	// 	s.h.Range(func(k, v any) bool {
	// 		keys = append(keys, k.(string))
	// 		return true
	// 	})
	// 	sort.Slice(keys, func(i, j int) bool {
	// 		lhs, ok := s.h.Load(keys[i])
	// 		if !ok {
	// 			return false
	// 		}
	// 		rhs, ok := s.h.Load(keys[j])
	// 		if !ok {
	// 			return false
	// 		}
	// 		return lhs.(*nostr.Event).CreatedAt.Time().Before(rhs.(*nostr.Event).CreatedAt.Time())
	// 	})
	//
	// 	var highlights []*nip84.Highlight
	// 	for _, key := range keys {
	// 		vv, ok := s.h.Load(key)
	// 		if !ok {
	// 			continue
	// 		}
	// 		a, err := nip84.ToHighlight(vv.(*nostr.Event))
	// 		if err != nil {
	// 			log.Fatalln(err)
	// 		}
	// 		highlights = append(highlights, &a)
	// 	}

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
