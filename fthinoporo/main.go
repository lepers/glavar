package main

import "flag"

var (
	ingestFlag = flag.Bool("ingest", false, "Import from Autumn")
	indexFlag  = flag.Bool("index", false, "Build a bleve cache")

	// search capabilities
	findFlag  = flag.String("find", "", "Fuzzy finder over log")
	userFlag  = flag.String("user", "", "Limit search to user")
	deltaFlag = flag.Int("dt", 0, "Capture context (abs(t-t0) <= dt)")
)

func main() {
	flag.Parse()

	switch {
	case *ingestFlag:
		ingest()
	case *indexFlag:
		index()
	case *findFlag != "":
		find(*findFlag)
	default:
		flag.Usage()
	}
}
