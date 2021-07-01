package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strconv"
	"time"

	"github.com/pkg/errors"
	tele "gopkg.in/tucnak/telebot.v3"
)

var (
	outbound *http.Client

	bot *tele.Bot

	this = &struct {
		*tele.User `json:"user"`

		Cunts map[int]User `json:"cunts"`

		UID  string `json:"uid"`
		SID  string `json:"sid"`
		CSRF string `json:"csrf_token"`
		LM   int    `json:"last_message_id"`
	}{
		Cunts: make(map[int]User),
	}

	loggedIn = make(chan struct{})
	// time since last error
	errt = time.Now().Add(-time.Hour)
)

func primo() {
	<-loggedIn
	for {
		if err := poll(); err != nil {
			if time.Now().Sub(errt) > 10*time.Minute {
				bot.Send(this, fmt.Sprintf(errorCorr, err))
				errt = time.Now()
			}
		}
		<-time.After(5 * time.Second)
	}
}

func main() {
	jar, _ := cookiejar.New(nil)
	outbound = &http.Client{Jar: jar}

	var err error

	bot, err = tele.NewBot(tele.Settings{
		Token:     os.Getenv("BOT_TOKEN"),
		Poller:    &tele.LongPoller{Timeout: 5 * time.Second},
		ParseMode: tele.ModeMarkdownV2,
	})
	if err != nil {
		panic(err)
	}

	bot.Handle("/start", func(c tele.Context) error {
		return c.Reply(startCorr)
	})

	bot.Handle("/login", func(c tele.Context) error {
		args := c.Args()
		if len(args) != 2 {
			return c.Reply(naxyuCorr)
		}
		if err := login(args[0], args[1]); err != nil {
			return c.Reply(err.Error())
		}
		this.User = c.Sender()
		save()
		close(loggedIn)
		return nil
	})

	go primo()
	bot.Start()
}

const config = "glavar.json"

func load() {
	b, _ := ioutil.ReadFile(config)
	json.Unmarshal(b, this)
}
func save() {
	f, err := os.OpenFile(config, os.O_TRUNC, 0666)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(this)
}

func login(username, password string) error {
	const path = "https://leprosorium.ru/ajax/auth/login/"
	req, _ := http.NewRequest("POST", path, nil)
	headers := map[string]string{
		"Accept":          "*/*",
		"Accept-Encoding": "gzip, deflate, br",
		"Accept-Language": "en-US,en;q=0.9,ru;q=0.8",
		"Connection":      "keep-alive",
		"Content-Length":  "0",
		"Host":            "leprosorium.ru",
		"Origin":          "https://leprosorium.ru",
		"Referer":         "https://leprosorium.ru/login/",
		"Sec-Fetch-Dest":  "empty",
		"Sec-Fetch-Mode":  "cors",
		"Sec-Fetch-Site":  "same-origin",
		"User-Agent":      "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/86.0.4240.75 Safari/537.36",
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	form := map[string]string{
		"username":             username,
		"password":             password,
		"g-recaptcha-response": "",
	}
	for k, v := range form {
		req.PostForm.Add(k, v)
	}

	resp, err := outbound.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	csrf := &struct {
		Status string `json:"status"`
		Token  string `json:"csrf_token"`
	}{}
	err = json.NewDecoder(resp.Body).Decode(csrf)
	if err != nil {
		return errors.Wrap(err, "csrf could not be decoded")
	}
	if csrf.Status != "OK" {
		return errors.New("NOT OK")
	}
	this.CSRF = csrf.Token
	for _, cookie := range resp.Cookies() {
		switch cookie.Name {
		case "uid":
			this.UID = cookie.Value
		case "sid":
			this.SID = cookie.Value
		default:
		}
	}
	return nil
}

func poll() error {
	const path = "https://leprosorium.ru/ajax/chat/load/"

	req, _ := http.NewRequest("POST", path, nil)
	headers := map[string]string{
		"Accept":          "*/*",
		"Accept-Encoding": "gzip, deflate, br",
		"Accept-Language": "en-US,en;q=0.9,ru;q=0.8",
		"Connection":      "keep-alive",
		"Content-Length":  "0",
		"Host":            "leprosorium.ru",
		"Origin":          "https://leprosorium.ru",
		"Referer":         "https://leprosorium.ru/",
		"Sec-Fetch-Dest":  "empty",
		"Sec-Fetch-Mode":  "cors",
		"Sec-Fetch-Site":  "same-origin",
		"Cookie":          fmt.Sprintf("wikilepro_session=%s; uid=%s; sid=%s", "", this.UID, this.SID),
		"User-Agent":      "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/86.0.4240.75 Safari/537.36",
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	form := map[string]string{
		"last_message_id": strconv.Itoa(this.LM),
		"csrf_token":      this.CSRF,
	}
	for k, v := range form {
		req.PostForm.Add(k, v)
	}

	resp, err := outbound.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var schema struct {
		Messages []Message `json:"messages"`
	}
	err = json.NewDecoder(resp.Body).Decode(&schema)
	if err != nil {
		return errors.Wrap(err, "updates could not be decoded")
	}

	defer save()
	for _, msg := range schema.Messages {
		payload := fmt.Sprintf(messageCorr, msg.User.Login, msg.Body)
		send := func() (err error) {
			_, err = bot.Send(this, payload)
			return
		}
		if err := send(); err != nil {
			err = send()
			if err != nil {
				panic(err)
			}
		}
		this.LM = msg.ID
		this.Cunts[msg.User.ID] = msg.User
	}
	return nil
}
