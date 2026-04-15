package main

import (
	"embed"
	"flag"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/pprof"
	"strconv"

	zim "github.com/justinstimatze/gozim"
	lru "github.com/hashicorp/golang-lru/v2"
)

type ResponseType int8

const (
	RedirectResponse ResponseType = iota
	DataResponse
	NoResponse
)

type CachedResponse struct {
	ResponseType ResponseType
	Data         []byte
	MimeType     string
}

type Server struct {
	lib       *zim.Library
	cache     *lru.Cache[string, CachedResponse]
	templates *template.Template
}

var (
	port       = flag.Int("port", -1, "port to listen to, read HOST env if not specified, default to 8080 otherwise")
	zimPath    = flag.String("path", "", "path for a single zim file")
	zimDir     = flag.String("dir", "", "path to a directory of zim files")
	indexPath  = flag.String("index", "", "path for the index file (single-file mode only)")
	mmapFlag   = flag.Bool("mmap", false, "use mmap")
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")

	//go:embed static
	staticFS embed.FS

	//go:embed templates/*
	templateFS embed.FS
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	flag.Parse()
	if *zimPath == "" && *zimDir == "" {
		log.Fatal("provide -path or -dir")
	}
	if *zimPath != "" && *zimDir != "" {
		log.Fatal("-path and -dir are mutually exclusive")
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()

		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		go func() {
			for range c {
				pprof.StopCPUProfile()
				os.Exit(1)
			}
		}()
	}

	var opts []zim.Option
	if *mmapFlag {
		log.Println("Using mmap")
		opts = append(opts, zim.WithMmap())
	}

	var lib *zim.Library
	if *zimDir != "" {
		var err error
		lib, err = zim.OpenLibrary(*zimDir, opts...)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		// Single-file mode: create a temp dir with a symlink so OpenLibrary works
		lib = mustLoadSingle(*zimPath, *indexPath, opts...)
	}
	defer lib.Close()

	for _, e := range lib.Errors() {
		log.Printf("warning: %v", e)
	}
	for slug, entry := range lib.Entries() {
		idx := ""
		if entry.IndexPath != "" {
			idx = " (indexed)"
		}
		log.Printf("loaded: %s → %s%s", slug, filepath.Base(entry.Path), idx)
	}

	tpls, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		log.Fatal(err)
	}

	cache, _ := lru.New[string, CachedResponse](40)
	srv := &Server{lib: lib, cache: cache, templates: tpls}

	mux := http.NewServeMux()
	fileServer := http.FileServer(http.FS(staticFS))
	mux.Handle("/static/", fileServer)
	mux.HandleFunc("/robots.txt", robotHandler)
	mux.HandleFunc("/about/", makeGzipHandler(srv.aboutHandler))
	// Register per-archive routes with literal prefixes to avoid
	// wildcard conflicts with /static/ and /about/ in Go's ServeMux.
	reserved := map[string]bool{"static": true, "about": true}
	for slug := range lib.Entries() {
		if reserved[slug] {
			log.Printf("warning: slug %q conflicts with reserved path, skipping", slug)
			continue
		}
		mux.HandleFunc("/"+slug+"/zim/", makeGzipHandler(srv.zimHandler))
		mux.HandleFunc("/"+slug+"/search/", makeGzipHandler(srv.searchHandler))
		mux.HandleFunc("/"+slug+"/browse/", makeGzipHandler(srv.browseHandler))
		mux.HandleFunc("/"+slug+"/", makeGzipHandler(srv.archiveHomeHandler))
	}
	mux.HandleFunc("/", makeGzipHandler(srv.libraryHandler))

	listenPath := ":8080"
	if len(os.Getenv("PORT")) > 0 {
		listenPath = ":" + os.Getenv("PORT")
	}
	if port != nil && *port > 0 {
		listenPath = ":" + strconv.Itoa(*port)
	}

	log.Println("Listening on", listenPath)
	if err := http.ListenAndServe(listenPath, mux); err != nil {
		log.Fatal(err)
	}
}

// mustLoadSingle wraps a single ZIM file into a Library.
// If -index is provided, it creates a symlink to the index next to the ZIM.
func mustLoadSingle(path, idxPath string, opts ...zim.Option) *zim.Library {
	// Create a temp dir with a symlink to the ZIM file
	dir, err := os.MkdirTemp("", "gozim-single-*")
	if err != nil {
		log.Fatal(err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		log.Fatal(err)
	}

	name := filepath.Base(path)
	if err := os.Symlink(absPath, filepath.Join(dir, name)); err != nil {
		log.Fatal(err)
	}

	// If index path is provided, symlink it next to the ZIM for auto-discovery
	if idxPath != "" {
		absIdx, err := filepath.Abs(idxPath)
		if err != nil {
			log.Fatal(err)
		}
		stem := name[:len(name)-len(filepath.Ext(name))]
		// Detect if it's a bleve dir or idx file
		fi, err := os.Stat(absIdx)
		if err != nil {
			log.Fatal(err)
		}
		var linkName string
		if fi.IsDir() {
			linkName = stem + ".bleve"
		} else {
			linkName = stem + ".idx"
		}
		if err := os.Symlink(absIdx, filepath.Join(dir, linkName)); err != nil {
			log.Fatal(err)
		}
	}

	lib, err := zim.OpenLibrary(dir, opts...)
	if err != nil {
		log.Fatal(err)
	}
	return lib
}
