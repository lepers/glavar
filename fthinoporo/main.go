package main

import "flag"

var (
	ingestFlag = flag.Bool("ingest", false, "Import from Autumn")
	indexFlag  = flag.Bool("index", false, "Build a bleve cache")
)

func main() {
	flag.Parse()

	switch {
	case *ingestFlag:
		ingest()
	case *indexFlag:
		index()
	default:
		flag.Usage()
	}
}
