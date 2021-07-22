package main

import (
	"database/sql"
	"fmt"
	"math/rand"
	"runtime"
	"strconv"
	"time"

	"github.com/blevesearch/bleve"
)

func index() {
	rand.Seed(time.Now().UnixNano())

	mapping := bleve.NewIndexMapping()
	idx, err := bleve.New("index", mapping)
	if err != nil {
		panic(err)
	}
	defer idx.Close()

	db, err := sql.Open("sqlite3", "logec.sqlite")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	data, err := db.Query("SELECT * FROM logec ORDER BY time ASC")
	if err != nil {
		panic(err)
	}

	N, pipe := 0, make(chan M, 1000)

	cpu := runtime.NumCPU()
	fmt.Println("cpu count:", cpu)

	var start time.Time
	for i := 0; i < cpu; i++ {
		go func(i int) {
			n := 0
			for m := range pipe {
				idx.Index(strconv.FormatInt(m.T, 10), m)
				n++
				N++

				if rand.Intn(1000) == 7 {
					fmt.Printf("[%v] indexed:%d, total:%d\n",
						time.Now().Sub(start), n, N)
				}
			}
		}(i)
	}

	fmt.Println("loading data")
	start = time.Now()
	for data.Next() {
		var m M
		err = data.Scan(&m.T, &m.Login, &m.Text)
		if err != nil {
			fmt.Println("!!", err)
			return
		}
		pipe <- m
		if rand.Intn(1000) == 7 {
			t := time.Unix(0, m.T).Format(time.RFC822)
			fmt.Println(t)
		}
	}

	close(pipe)
}
