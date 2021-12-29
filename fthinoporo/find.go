package main

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/blevesearch/bleve"
	"github.com/jmoiron/sqlx"
)

const BUF = 10000

func find(query string) {
	index, _ := bleve.Open("../logec")

	order := bleve.NewFuzzyQuery(query)
	req := bleve.NewSearchRequest(order)
	req.Size = BUF
	res, _ := index.Search(req)
	if res.MaxScore == 0 {
		fmt.Println("no results")
		return
	}

	is := make([]int64, 0, len(res.Hits))
	score := make(map[int64]float64)
	for _, hit := range res.Hits {
		i, _ := strconv.ParseInt(hit.ID, 10, 64)
		score[i] = hit.Score
		is = append(is, i)
	}

	db, err := sqlx.Connect("sqlite3", logecSqlite)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	q, args, _ := sqlx.In("SELECT * FROM logec WHERE tid IN (?)", is)
	q = db.Rebind(q)
	rows, err := db.Query(q, args...)
	if err != nil {
		panic(err)
	}

	ms := make([]M, 0, BUF)
	for rows.Next() {
		var m M
		rows.Scan(&m.T, &m.Login, &m.Text)
		ms = append(ms, m)
	}

	sort.Slice(ms, func(i int, j int) bool {
		return score[ms[i].T] > score[ms[j].T]
	})

	for _, m := range ms {
		tf := time.Unix(m.T, 0).Format("02/01/06")
		fmt.Printf("%2.2f %s %s\n", score[m.T], tf, m)
	}
}
