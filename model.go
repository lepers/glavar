package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Model struct {
	*User
	Name string `json:"name"`
	Busy bool   `json:"busy"`
}

type replyFeed struct {
	login  string
	bot    string
	prompt string
	result []string
	busy   bool
}

var (
	nologinRx = regexp.MustCompile(`^[\\p{L}\\d_-–—]+:`)
	nourlRx   = regexp.MustCompile(`\w+:\/{2}[\d\w-]+(\.[\d\w-]+)*(?:(?:\/[^\s/]*))*`)
	mites     = []string{
		"Delphis",
		"megalomaniac",
		"tearjerker",
		"mahinaci9",
		"miss_iceheart",
		"BLUEWATER",
		"Fulano",
	}

	goodp, badp []string
)

func readprompts(fname string) []string {
	f, _ := os.Open(fname)
	scanner := bufio.NewScanner(f)

	var vec []string
	for scanner.Scan() {
		vec = append(vec, scanner.Text())
	}
	return vec
}

func prefix4(author string) string {
	ismite := false
	for _, mite := range mites {
		if author == mite {
			ismite = true
			break
		}
	}
	// 50%
	if rand.Float32() > 0.5 {
		return ""
	}
	if ismite {
		badp = readprompts(datadir + "/bad.txt")
		return badp[rand.Intn(len(badp))]
	}
	goodp = readprompts(datadir + "/good.txt")
	return goodp[rand.Intn(len(goodp))]
}

func (model *Model) deliver(author string, res []string) {
	if author != "" {
		author += ": "
	}

	for i, line := range res {
		line = strings.TrimSpace(line)
		n := utf8.RuneCountInString(line)
		<-time.After(time.Duration(n/3) * time.Second)

		msg := line
		if i == 0 {
			msg = author + line
		}
		model.User.broadcast(msg)
		if model.T != nil {
			msg = ø(cue, model.Login, msg)
			bot.Send(model.T, msg)
		}
	}
}

func (model *Model) feed(author, body string) []string {
	body = nologinRx.ReplaceAllString(body, "")
	body = nourlRx.ReplaceAllString(body, "")
	body = strings.TrimSpace(body)
	// if utf8.RuneCountInString(body) < 25 {
	// 	return
	// }

	limit := 40 + rand.Intn(50)
	path := "http://" + model.Name + ":8000/prompt"
	req, _ := http.NewRequest("GET", path, nil)
	q := req.URL.Query()
	q.Add("q", body)
	q.Add("prefix", prefix4(author))
	q.Add("limit", strconv.Itoa(limit))
	req.URL.RawQuery = q.Encode()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("%s error: %v\n", model.User.Login, err)
		return nil
	}
	defer resp.Body.Close()
	var result struct {
		Lines []string `json:"result"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	res := result.Lines

	if len(res) < 1 {
		return nil
	}
	if len(res) > 1 {
		res = res[:len(res)-1]
	}
	if len(res) > 2 && rand.Float32() < 0.5 {
		res = res[1:]
	}
	for i := range res {
		res[i] = strings.ToLower(res[i])
	}
	return res
	// go model.deliver(author, res)
}
