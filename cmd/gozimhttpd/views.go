package main

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	zim "github.com/justinstimatze/gozim"
)

const (
	ArticlesPerPage = 16
)

func (s *Server) cacheLookup(key string) (*CachedResponse, bool) {
	if c, ok := s.cache.Get(key); ok {
		return &c, ok
	}
	return nil, false
}

func (s *Server) handleCachedResponse(cr *CachedResponse, w http.ResponseWriter, r *http.Request, slug string) {
	if cr.ResponseType == RedirectResponse {
		target := "/" + slug + "/zim/" + string(cr.Data)
		log.Printf("302 from %s to %s\n", r.URL.Path, target)
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	} else if cr.ResponseType == NoResponse {
		log.Printf("404 %s\n", r.URL.Path)
		http.NotFound(w, r)
	} else if cr.ResponseType == DataResponse {
		log.Printf("200 %s\n", r.URL.Path)
		w.Header().Set("Content-Type", cr.MimeType)
		w.Header().Set("Cache-control", "public, max-age=1350000")
		w.Write(cr.Data)
	}
}

func (s *Server) resolveArchive(w http.ResponseWriter, r *http.Request) (*zim.LibraryEntry, string) {
	slug := r.PathValue("slug")
	entry, ok := s.lib.Get(slug)
	if !ok {
		http.NotFound(w, r)
		return nil, ""
	}
	return entry, slug
}

func (s *Server) zimHandler(w http.ResponseWriter, r *http.Request) {
	entry, slug := s.resolveArchive(w, r)
	if entry == nil {
		return
	}

	prefix := "/" + slug + "/zim/"
	url := strings.TrimPrefix(r.URL.Path, prefix)
	cacheKey := slug + ":" + url

	if cr, iscached := s.cacheLookup(cacheKey); iscached {
		s.handleCachedResponse(cr, w, r, slug)
		return
	}

	e, err := entry.Archive.GetEntryByFullPath(url)
	if err != nil {
		s.cache.Add(cacheKey, CachedResponse{ResponseType: NoResponse})
	} else if e.IsRedirect() {
		target, err := e.RedirectTarget()
		if err != nil {
			s.cache.Add(cacheKey, CachedResponse{ResponseType: NoResponse})
		} else {
			s.cache.Add(cacheKey, CachedResponse{
				ResponseType: RedirectResponse,
				Data:         []byte(target.FullPath()),
			})
		}
	} else {
		data, err := e.Content()
		if err != nil {
			s.cache.Add(cacheKey, CachedResponse{ResponseType: NoResponse})
		} else {
			s.cache.Add(cacheKey, CachedResponse{
				ResponseType: DataResponse,
				Data:         data,
				MimeType:     e.MimeType(),
			})
		}
	}

	if cr, iscached := s.cacheLookup(cacheKey); iscached {
		s.handleCachedResponse(cr, w, r, slug)
	}
}

func (s *Server) libraryHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// If only one archive, redirect to its home
	if s.lib.Len() == 1 {
		slugs := s.lib.Slugs()
		http.Redirect(w, r, "/"+slugs[0]+"/", http.StatusFound)
		return
	}

	type archiveInfo struct {
		Slug        string
		Title       string
		Description string
		Language    string
		EntryCount  string
		HomeURL     string
	}

	var archives []archiveInfo
	for slug, entry := range s.lib.Entries() {
		title := entry.Archive.Title()
		if title == "" {
			title = filepath.Base(entry.Path)
		}
		archives = append(archives, archiveInfo{
			Slug:        slug,
			Title:       title,
			Description: entry.Archive.Description(),
			Language:    entry.Archive.Language(),
			EntryCount:  strconv.FormatUint(uint64(entry.Archive.EntryCount()), 10),
			HomeURL:     "/" + slug + "/",
		})
	}

	d := map[string]interface{}{
		"Archives": archives,
	}

	if err := s.templates.ExecuteTemplate(w, "library.html", d); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func (s *Server) archiveHomeHandler(w http.ResponseWriter, r *http.Request) {
	entry, slug := s.resolveArchive(w, r)
	if entry == nil {
		return
	}

	// If the path has more after /{slug}/, redirect to zim handler
	trimmed := strings.TrimPrefix(r.URL.Path, "/"+slug+"/")
	if trimmed != "" {
		http.Redirect(w, r, "/"+slug+"/zim/"+trimmed, http.StatusMovedPermanently)
		return
	}

	var mainURL string
	var hasMainPage bool

	mainEntry, err := entry.Archive.MainEntry()
	if err == nil {
		hasMainPage = true
		mainURL = "/" + slug + "/zim/" + mainEntry.FullPath()
	}

	d := map[string]interface{}{
		"Prefix":      "/" + slug,
		"Path":        filepath.Base(entry.Path),
		"Count":       strconv.FormatUint(uint64(entry.Archive.EntryCount()), 10),
		"IsIndexed":   entry.IndexPath != "",
		"HasMainPage": hasMainPage,
		"MainURL":     mainURL,
		"ShowLibrary": s.lib.Len() > 1,
	}

	if err := s.templates.ExecuteTemplate(w, "index.html", d); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func (s *Server) aboutHandler(w http.ResponseWriter, r *http.Request) {
	d := map[string]interface{}{}
	if err := s.templates.ExecuteTemplate(w, "about.html", d); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func (s *Server) searchHandler(w http.ResponseWriter, r *http.Request) {
	entry, slug := s.resolveArchive(w, r)
	if entry == nil {
		return
	}

	prefix := "/" + slug

	pageString := r.FormValue("page")
	pageNumber, _ := strconv.Atoi(pageString)
	previousPage := pageNumber - 1
	if pageNumber == 0 {
		previousPage = 0
	}
	nextPage := pageNumber + 1
	q := r.FormValue("search_data")
	d := map[string]interface{}{
		"Prefix":       prefix,
		"Query":        q,
		"Path":         filepath.Base(entry.Path),
		"Page":         pageNumber,
		"PreviousPage": previousPage,
		"NextPage":     nextPage,
	}

	if entry.IndexPath == "" {
		if err := s.templates.ExecuteTemplate(w, "searchNoIdx.html", d); err != nil {
			http.Error(w, err.Error(), 500)
		}
		return
	}

	if q == "" {
		if err := s.templates.ExecuteTemplate(w, "search.html", d); err != nil {
			http.Error(w, err.Error(), 500)
		}
		return
	}

	if len(q) > 500 {
		http.Error(w, "query too long", 400)
		return
	}

	itemCount := 20
	opts := []zim.SearchOption{
		zim.WithOffset(itemCount * pageNumber),
	}
	if entry.IndexPath != "" {
		opts = append(opts, zim.WithIndexPath(entry.IndexPath))
	}

	results, err := entry.Archive.Search(q, itemCount, opts...)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	if len(results) > 0 {
		d["Info"] = fmt.Sprintf("%d results for query [%s]", len(results), q)

		var l []map[string]string
		for _, r := range results {
			l = append(l, map[string]string{
				"Score": strconv.FormatFloat(r.Score, 'f', 1, 64),
				"Title": r.Entry.Title(),
				"URL":   prefix + "/zim/" + r.Entry.FullPath(),
			})
		}
		d["Hits"] = l
	} else {
		d["Info"] = fmt.Sprintf("No match for [%s]", q)
		d["Hits"] = 0
	}

	if err := s.templates.ExecuteTemplate(w, "searchResult.html", d); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func (s *Server) browseHandler(w http.ResponseWriter, r *http.Request) {
	entry, slug := s.resolveArchive(w, r)
	if entry == nil {
		return
	}

	prefix := "/" + slug
	var page, previousPage, nextPage int

	if p := r.URL.Query().Get("page"); p != "" {
		page, _ = strconv.Atoi(p)
	}

	entryCount := int(entry.Archive.EntryCount())
	if page*ArticlesPerPage-1 >= entryCount {
		http.NotFound(w, r)
		return
	}

	type browseEntry struct {
		Title string
		URL   string
	}

	entries := make([]browseEntry, 0, ArticlesPerPage)
	for i := page * ArticlesPerPage; i < page*ArticlesPerPage+ArticlesPerPage && i < entryCount; i++ {
		e, err := entry.Archive.GetEntryByIndex(uint32(i))
		if err != nil {
			continue
		}
		title := e.Title()
		if title == "" {
			title = e.FullPath()
		}
		entries = append(entries, browseEntry{Title: title, URL: prefix + "/zim/" + e.FullPath()})
	}

	if page == 0 {
		previousPage = 0
	} else {
		previousPage = page - 1
	}

	if page*ArticlesPerPage-1 >= entryCount {
		nextPage = page
	} else {
		nextPage = page + 1
	}

	d := map[string]interface{}{
		"Prefix":       prefix,
		"Page":         page,
		"PreviousPage": previousPage,
		"NextPage":     nextPage,
		"Articles":     entries,
	}

	if err := s.templates.ExecuteTemplate(w, "browse.html", d); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func robotHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "User-agent: *\nDisallow: /\n")
}
