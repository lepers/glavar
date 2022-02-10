package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v4"
	"github.com/sethvargo/go-retry"

	tele "gopkg.in/telebot.v3"
)

var _postgres *pgx.Conn

func makedb() *pgx.Conn {
	// always attempt to make a connection
	if _postgres == nil || _postgres.IsClosed() {
		_postgres = makeconnect()
	}
	return _postgres
}

func makeconnect() (conn *pgx.Conn) {
	var (
		bg = context.Background()
		t0 = time.Now()
		bo = backoff(100*time.Millisecond, time.Minute)
	)

	retry.Do(bg, bo, func(c context.Context) (err error) {
		conn, err = pgx.Connect(c, os.Getenv("DATABASE_URL"))
		if err != nil {
			fmt.Printf("+%v makeconnect(%v)\n", time.Since(t0), err)
		}
		return retry.RetryableError(err)
	})
	return
}

// H returns a simple 64-bit FNV hash.
func H(value ...string) uint64 {
	h := fnv.New64()
	for _, v := range value {
		h.Write([]byte(v))
	}
	return h.Sum64()
}

func decouple(message *tele.Message) (user, text string, ok bool) {
	if message == nil {
		return
	}

	m := message.Text
	i := strings.Index(m, "<")
	j := strings.Index(m, ">")
	if i < 0 || j < 0 {
		return
	}

	return m[i+1 : j], m[j+2:], true
}

func backoff(start, max time.Duration) retry.Backoff {
	fib := retry.NewFibonacci(start)
	return retry.WithMaxDuration(max, fib)
}

func getuser(c tele.Context) (*User, error) {
	tid := c.Sender().ID
	u, ok := this.Users[tid]
	if !ok || !u.logged() {
		return nil, ErrNotLogged
	}
	return u, nil
}

func getleper(name string) *User {
	for _, cunt := range this.Users {
		if !cunt.logged() {
			continue
		}
		if cunt.Login == name {
			return cunt
		}
	}
	return nil
}

func ismodel(name string) bool {
	for _, model := range this.Models {
		if model.User.Login == name {
			return true
		}
	}
	return false
}

func load() {
	b, _ := ioutil.ReadFile(datadir + "/glavar.json")
	json.Unmarshal(b, &this)
}

func save() {
	f, err := os.Create(datadir + "/glavar.json")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "\t")
	enc.Encode(&this)
}
