package main

import (
	"fmt"
	"log"
	"net/http"
	"path"
	"strconv"

	zim "github.com/justinstimatze/gozim"
)

const (
	ArticlesPerPage = 16
)

func cacheLookup(url string) (*CachedResponse, bool) {
	if c, ok := cache.Get(url); ok {
		return &c, ok
	}
	return nil, false
}

func handleCachedResponse(cr *CachedResponse, w http.ResponseWriter, r *http.Request) {
	if cr.ResponseType == RedirectResponse {
		log.Printf("302 from %s to %s\n", r.URL.Path, "zim/"+string(cr.Data))
		http.Redirect(w, r, "/zim/"+string(cr.Data), http.StatusMovedPermanently)
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

func zimHandler(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Path[5:]
	if cr, iscached := cacheLookup(url); iscached {
		handleCachedResponse(cr, w, r)
		return
	}

	entry, err := archive.GetEntryByFullPath(url)
	if err != nil {
		cache.Add(url, CachedResponse{ResponseType: NoResponse})
	} else if entry.IsRedirect() {
		target, err := entry.RedirectTarget()
		if err != nil {
			cache.Add(url, CachedResponse{ResponseType: NoResponse})
		} else {
			cache.Add(url, CachedResponse{
				ResponseType: RedirectResponse,
				Data:         []byte(target.FullPath()),
			})
		}
	} else {
		data, err := entry.Content()
		if err != nil {
			cache.Add(url, CachedResponse{ResponseType: NoResponse})
		} else {
			cache.Add(url, CachedResponse{
				ResponseType: DataResponse,
				Data:         data,
				MimeType:     entry.MimeType(),
			})
		}
	}

	if cr, iscached := cacheLookup(url); iscached {
		handleCachedResponse(cr, w, r)
	}
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Redirect(w, r, "/zim"+r.URL.Path, http.StatusMovedPermanently)
		return
	}

	var mainURL string
	var hasMainPage bool

	mainEntry, err := archive.MainEntry()
	if err == nil {
		hasMainPage = true
		mainURL = "/zim/" + mainEntry.FullPath()
	}

	d := map[string]interface{}{
		"Path":        path.Base(*zimPath),
		"Count":       strconv.FormatUint(uint64(archive.EntryCount()), 10),
		"IsIndexed":   idx,
		"HasMainPage": hasMainPage,
		"MainURL":     mainURL,
	}

	if err := templates.ExecuteTemplate(w, "index.html", d); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func aboutHandler(w http.ResponseWriter, r *http.Request) {
	d := map[string]interface{}{}
	if err := templates.ExecuteTemplate(w, "about.html", d); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func searchHandler(w http.ResponseWriter, r *http.Request) {
	pageString := r.FormValue("page")
	pageNumber, _ := strconv.Atoi(pageString)
	previousPage := pageNumber - 1
	if pageNumber == 0 {
		previousPage = 0
	}
	nextPage := pageNumber + 1
	q := r.FormValue("search_data")
	d := map[string]interface{}{
		"Query":        q,
		"Path":         path.Base(*zimPath),
		"Page":         pageNumber,
		"PreviousPage": previousPage,
		"NextPage":     nextPage,
	}

	if !idx {
		if err := templates.ExecuteTemplate(w, "searchNoIdx.html", d); err != nil {
			http.Error(w, err.Error(), 500)
		}
		return
	}

	if q == "" {
		if err := templates.ExecuteTemplate(w, "search.html", d); err != nil {
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
	if *indexPath != "" {
		opts = append(opts, zim.WithIndexPath(*indexPath))
	}

	results, err := archive.Search(q, itemCount, opts...)
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
				"URL":   "/zim/" + r.Entry.FullPath(),
			})
		}
		d["Hits"] = l
	} else {
		d["Info"] = fmt.Sprintf("No match for [%s]", q)
		d["Hits"] = 0
	}

	if err := templates.ExecuteTemplate(w, "searchResult.html", d); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func browseHandler(w http.ResponseWriter, r *http.Request) {
	var page, previousPage, nextPage int

	if p := r.URL.Query().Get("page"); p != "" {
		page, _ = strconv.Atoi(p)
	}

	entryCount := int(archive.EntryCount())
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
		e, err := archive.GetEntryByIndex(uint32(i))
		if err != nil {
			continue
		}
		title := e.Title()
		if title == "" {
			title = e.FullPath()
		}
		entries = append(entries, browseEntry{Title: title, URL: "/zim/" + e.FullPath()})
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
		"Page":         page,
		"PreviousPage": previousPage,
		"NextPage":     nextPage,
		"Articles":     entries,
	}

	if err := templates.ExecuteTemplate(w, "browse.html", d); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func robotHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "User-agent: *\nDisallow: /\n")
}
