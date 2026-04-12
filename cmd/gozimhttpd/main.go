package main

import (
	"embed"
	"flag"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
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

var (
	port       = flag.Int("port", -1, "port to listen to, read HOST env if not specified, default to 8080 otherwise")
	zimPath    = flag.String("path", "", "path for the zim file")
	indexPath  = flag.String("index", "", "path for the index file")
	mmapFlag   = flag.Bool("mmap", false, "use mmap")
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")

	archive *zim.Archive
	cache   *lru.Cache[string, CachedResponse]
	idx     bool

	templates *template.Template

	//go:embed static
	staticFS embed.FS

	//go:embed templates/*
	templateFS embed.FS
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	flag.Parse()
	if *zimPath == "" {
		log.Fatal("provide a zim file path")
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

	// Open the archive
	var opts []zim.Option
	if *mmapFlag {
		log.Println("Using mmap")
		opts = append(opts, zim.WithMmap())
	}

	var err error
	archive, err = zim.Open(*zimPath, opts...)
	if err != nil {
		log.Fatal(err)
	}
	defer archive.Close()

	// Check for search index
	if indexPath != nil && *indexPath != "" {
		if _, err := os.Stat(*indexPath); err != nil {
			log.Fatal(err)
		}
		idx = true
	}

	tpls, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		log.Fatal(err)
	}
	templates = tpls

	fileServer := http.FileServer(http.FS(staticFS))
	http.Handle("/static/", fileServer)

	http.HandleFunc("/zim/", makeGzipHandler(zimHandler))
	http.HandleFunc("/search/", makeGzipHandler(searchHandler))
	http.HandleFunc("/browse/", makeGzipHandler(browseHandler))
	http.HandleFunc("/about/", makeGzipHandler(aboutHandler))
	http.HandleFunc("/robots.txt", robotHandler)
	http.HandleFunc("/", makeGzipHandler(homeHandler))

	cache, _ = lru.New[string, CachedResponse](40)

	listenPath := ":8080"
	if len(os.Getenv("PORT")) > 0 {
		listenPath = ":" + os.Getenv("PORT")
	}
	if port != nil && *port > 0 {
		listenPath = ":" + strconv.Itoa(*port)
	}

	log.Println("Listening on", listenPath)
	if err := http.ListenAndServe(listenPath, nil); err != nil {
		log.Fatal(err)
	}
}
