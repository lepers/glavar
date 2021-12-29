package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"jaytaylor.com/html2text"
)

const (
	sql_CREATE_TABLE_logec = `
CREATE TABLE logec (tid, date, login, text);`

	sql_INSERT_MESSAGE = `
INSERT INTO logec (tid, date, login, text) VALUES (?, ?, ?, ?);`
)

type M struct {
	T     int64  `sql:"tid"`
	Date  string `sql:"date"`
	Login string `sql:"login"`
	Text  string `sql:"text"`
}

func (m M) String() string {
	return fmt.Sprintf("<%s> %s", m.Login, m.Text)
}

func (m M) uniq() string {
	return strconv.FormatInt(m.T, 10)+m.Login+m.Text
}

func feed(bus chan M) {
	path := os.Getenv("LOGEC")
	for i := 1; ; i++ {
		fname := fmt.Sprintf("%s/%d.html", path, i)
		b, err := ioutil.ReadFile(fname)
		if err != nil {
			break
		}
		b = b[bytes.Index(b, []byte("<tbody>")):]
		b = b[:bytes.Index(b, []byte("</tbody>"))]
		tr := strings.Split(string(b), "</tr>")
		for _, cellb := range tr {
			if strings.Contains(cellb, "data-total-pages") {
				continue
			}
			td := strings.Split(cellb, "</td>")
			if len(td) < 3 {
				continue
			}

			t, login, opus := gett(td[0]), get(td[1]), get(td[2])
			fmt.Println(t, login, opus)
			opus, _ = html2text.FromString(opus, html2text.Options{})
			//	Mon Jan 2 15:04:05 -0700 MST 2006
			tf := t.Format("2006/01/02 15:04")
			bus <- M{t.Unix(), tf, login, opus}
		}
	}
	close(bus)
}

func ingest() {
	db, err := sql.Open("sqlite3", "ingest.sqlite3")
	if err != nil {
		panic(err)
	}
	defer db.Close()
	db.Exec(sql_CREATE_TABLE_logec)

	bus, auto := make(chan M), make(chan M)
	go feed(bus)
	go func() {
		dedup := make([]M, 1, 200)
		uniq := make(map[string]bool)
		for m := range bus {
			id := m.uniq()
			if _, ok := uniq[id]; ok {
				continue
			} else {
				uniq[id] = true
			}

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
				qsize := 60.0/float64(len(dedup))
				for i, m := range dedup {
					if m.T == 0 {
						break
					}
					m.T -= int64(qsize*float64(i))
					auto <- m
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
			qsize := 60.0/float64(len(dedup))
			for i, m := range dedup {
				if m.T == 0 {
					break
				}
				m.T -= int64(qsize*float64(i))
				auto <- m
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

		_, err = stmt.Exec(m.T, m.Date, m.Login, m.Text)
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
