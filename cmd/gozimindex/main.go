package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"

	"github.com/PuerkitoBio/goquery"
	zim "github.com/justinstimatze/gozim"
)

var (
	path         = flag.String("path", "", "path for the zim file")
	indexPath    = flag.String("index", "", "path for the index directory")
	lang         = flag.String("lang", "", "language for indexation")
	batchSize    = flag.Int("batchsize", 1000, "size of bleve batches")
	indexContent = flag.Bool("content", false, "experimental: index the content of the page")
)

func main() {
	flag.Parse()

	if *path == "" {
		log.Fatal("provide a zim file path")
	}
	if *indexPath == "" {
		log.Fatal("provide a path for the index")
	}

	archive, err := zim.Open(*path)
	if err != nil {
		log.Fatal(err)
	}
	defer archive.Close()

	analyzer := "standard"
	switch *lang {
	case "fr":
		analyzer = "frnostemm"
	case "en":
		analyzer = "ennostemm"
	case "ar", "ca", "ckb", "el", "eu", "gl", "hy", "in", "ja", "bg", "cjk", "cs", "fa", "ga", "hi", "id", "it", "pt":
		analyzer = *lang
	case "":
	default:
		log.Fatal("unsupported language")
	}

	opts := []zim.SearchOption{
		zim.WithIndexPath(*indexPath),
		zim.WithAnalyzer(analyzer),
		zim.WithBatchSize(*batchSize),
	}

	if *indexContent {
		opts = append(opts, zim.WithContentIndexing(func(data []byte) string {
			doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
			if err != nil {
				return ""
			}
			return doc.Text()
		}))
	}

	fmt.Printf("Building index for %s (%d entries)...\n", *path, archive.EntryCount())

	if err := archive.BuildIndex(opts...); err != nil {
		log.Fatal(err)
	}

	fmt.Println("Index built successfully at", *indexPath)
}
