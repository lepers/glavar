package main

import (
	"database/sql"
	"fmt"
	"runtime"
	"strconv"
	"sync"

	"github.com/blevesearch/bleve"
)

var logecSqlite = "import.sqlite"

func index() {
	mapping := bleve.NewIndexMapping()
	idx, err := bleve.New("index", mapping)
	if err != nil {
		panic(err)
	}
	defer idx.Close()

	db, err := sql.Open("sqlite3", logecSqlite)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	data, err := db.Query("SELECT * FROM logec ORDER BY tid DESC")
	if err != nil {
		panic(err)
	}

	const batchSize = 2500

	N, expect := 0, 1462075.0

	cpu := runtime.NumCPU()
	batches := make(chan []M, cpu)
	wg := &sync.WaitGroup{}
	for i := 0; i < cpu; i++ {
		go func() {
			wg.Add(1)
			defer wg.Done()
			for messages := range batches {
				b := idx.NewBatch()
				for _, m := range messages {
					b.Index(strconv.FormatInt(m.T, 10), m)
				}
				idx.Batch(b)
				N++
				fmt.Printf("[%3d%%] %d batches completed\n",
					int(float64(N*batchSize)/expect*100), N)
			}
		}()
	}

	A := make([]M, 0, batchSize)
	for data.Next() {
		var m M
		err = data.Scan(&m.T, &m.Date, &m.Login, &m.Text)
		if err != nil {
			fmt.Println("!!", err)
			return
		}

		A = append(A, m)

		if len(A) == cap(A) {
			batches <- A
			A = make([]M, 0, batchSize)
		}
	}

	if len(A) != 0 {
		batches <- A
	}

	close(batches)
	wg.Wait()
}
