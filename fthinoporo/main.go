package main

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"jaytaylor.com/html2text"
)

const (
	sql_CREATE_TABLE_logec = `
CREATE TABLE "logec" (
	"time"	INTEGER NOT NULL,
	"login"	TEXT,
	"text"	TEXT,
	PRIMARY KEY("time")
);`

	sql_INSERT_MESSAGE = `
INSERT INTO "logec" ("time", "login", "text") VALUES (?, ?, ?);`
)

type M struct {
	T     int64  `sql:"time"`
	Login string `sql:"login"`
	Text  string `sql:"text"`
}

func feed(bus chan M) {
	path := os.Getenv("LOGEC")
	for i := 1; ; i++ {
		b, err := ioutil.ReadFile(fmt.Sprintf("%s/%d.html", path, i))
		if err != nil {
			break
		}
		tr := strings.Split(string(b), "</tr>")
		for _, cellb := range tr {
			if strings.Contains(cellb, "data-total-pages") {
				continue
			}
			td := strings.Split(cellb, "</td>")
			if len(td) == 0 || td[0] == "" {
				continue
			}

			t, login, opus := gett(td[0]), get(td[1]), get(td[2])
			opus, _ = html2text.FromString(opus, html2text.Options{})
			bus <- M{t.UnixNano(), login, opus}
		}
	}
	close(bus)
}

func main() {
	os.Remove("logec.sqlite")
	db, err := sql.Open("sqlite3", "logec.sqlite")
	if err != nil {
		panic(err)
	}
	defer db.Close()
	if _, err := db.Exec(sql_CREATE_TABLE_logec); err != nil {
		panic(err)
	}

	bus, auto := make(chan M), make(chan M)
	go feed(bus)
	go func() {
		dedup := make([]M, 1, 200)
		for m := range bus {
			tail := dedup[len(dedup)-1]
			// duplicate
			if tail.Text == m.Text {
				continue
			}
			// to be ordered
			if tail.T == m.T {
				dedup = append(dedup, m)
				continue
			}

			if len(dedup) > 1 {
				qsize := time.Second / time.Duration(len(dedup))
				for i, mm := range dedup {
					if mm.T == 0 {
						break
					}
					mm.T -= int64(time.Duration(i) * qsize)
					auto <- mm
				}
				dedup = dedup[:1]
				dedup[0] = m
				continue
			}

			if dedup[0].T != 0 {
				auto <- dedup[0]
			}
			dedup[0] = m
		}
		if len(dedup) > 1 {
			qsize := time.Second / time.Duration(len(dedup))
			for i, mm := range dedup {
				if mm.T == 0 {
					break
				}
				mm.T -= int64(time.Duration(i) * qsize)
				auto <- mm
			}
		}
		close(auto)
	}()

	tx, _ := db.Begin()
	stmt, err := tx.Prepare(sql_INSERT_MESSAGE)
	if err != nil {
		panic(err)
	}
	defer stmt.Close()

	pushed := map[int64]bool{}
	for m := range auto {
		if _, ok := pushed[m.T]; ok {
			continue
		}
		_, err = stmt.Exec(m.T, m.Login, m.Text)
		if err != nil {
			fmt.Println("!", m, err)
			continue
		}
		pushed[m.T] = true
	}
	tx.Commit()
}

var timeRx = regexp.MustCompile(`data-param='([^']+)'`)

func gett(s string) time.Time {
	m := timeRx.FindAllStringSubmatch(s, -1)
	tstr := m[0][1]
	t, _ := time.Parse("2006-01-02 15:04", tstr)
	return t
}

func get(s string) string {
	y := strings.Split(s, "<td class='  '>")
	return strings.TrimSpace(strings.Join(y[1:], ""))
}
