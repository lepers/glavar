package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	tele "gopkg.in/tucnak/telebot.v3"
)

const config = "glavar.json"

var (
	outbound *http.Client

	bot *tele.Bot

	this = &struct {
		*tele.User `json:"user"`

		Username string `json:"username"`

		Cunts map[int]User `json:"cunts"`

		Cookies []*http.Cookie `json:"cookies"`

		UID  string `json:"uid"`
		SID  string `json:"sid"`
		CSRF string `json:"csrf_token"`
		LM   int    `json:"last_message_id"`

		Keywords []string `json:"keywords"`
	}{
		Cunts: make(map[int]User),
	}

	authorized = make(chan struct{})
	// time since last error
	errt     = time.Now().Add(-time.Hour)
	lepra, _ = url.Parse("https://leprosorium.ru")
	ø        = fmt.Sprintf
)

func primo() {
	if authorized != nil {
		<-authorized
	}
	for {
		if err := poll(); err != nil {
			if time.Now().Sub(errt) > 10*time.Minute {
				bot.Send(this, ø(errorCue, err))
				errt = time.Now()
			}
		}
		<-time.After(5 * time.Second)
	}
}

func main() {
	load()
	save()
	defer save()

	jar, _ := cookiejar.New(nil)
	if this.Cookies != nil {
		jar.SetCookies(lepra, this.Cookies)
		authorized = nil
	}
	outbound = &http.Client{Jar: jar}

	var err error

	bot, err = tele.NewBot(tele.Settings{
		Token:     os.Getenv("BOT_TOKEN"),
		Poller:    &tele.LongPoller{Timeout: 5 * time.Second},
		ParseMode: tele.ModeHTML,
	})
	if err != nil {
		panic(err)
	}

	bot.Handle("/start", func(c tele.Context) error {
		return c.Reply(startCue)
	})

	bot.Handle("/login", func(c tele.Context) error {
		args := c.Args()
		if len(args) != 2 {
			return c.Reply(naxyuCue)
		}
		if err := login(args[0], args[1]); err != nil {
			return c.Reply(err.Error())
		}
		this.User = c.Sender()
		save()
		close(authorized)
		return nil
	})

	bot.Handle("/keyword", func(c tele.Context) error {
		args := c.Args()
		if len(args) == 0 {
			return c.Reply(keywordCue)
		}
		this.Keywords = args
		save()
		return c.Reply(ø(keywordUpdatedCue, this.Keywords))
	})

	bot.Handle(tele.OnText, func(c tele.Context) error {
		const bufsize = 4 * 255 // utf8 * limit

		var b bytes.Buffer
		b.Grow(bufsize)

		message := c.Text()
		og := c.Message().ReplyTo
		if og != nil {
			i := strings.Index(og.Text, "<")
			j := strings.Index(og.Text, ">")
			if j > 0 {
				message = og.Text[i+1:j] + ": " + message
			}
		}
		for _, r := range message {
			b.WriteRune(r)
			if b.Len() == b.Cap() {
				if err := broadcast(b.String()); err != nil {
					// retry
					err = broadcast(b.String())
					if err != nil {
						return c.Reply(ø(errorCue, err))
					}
				}
				b.Reset()
			}
		}
		if b.Len() > 0 {
			if err := broadcast(b.String()); err != nil {
				// retry
				err = broadcast(b.String())
				if err != nil {
					return c.Reply(ø(errorCue, err))
				}
			}
		}
		return nil
	})

	bot.Handle(tele.OnPhoto, func(c tele.Context) error {
		const path = "https://idiod.video/api/upload.php"

		photo, err := bot.File(&c.Message().Photo.File)
		if err != nil {
			return c.Reply(ø(errorCue, err))
		}

		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		fw, _ := w.CreateFormFile("file", "image.jpg")
		if _, err = io.Copy(fw, photo); err != nil {
			return c.Reply(ø(errorCue, err))
		}
		w.Close()
		req, err := http.NewRequest("POST", path, &b)
		if err != nil {
			return c.Reply(ø(errorCue, err))
		}
		req.Header.Set("Content-Type", w.FormDataContentType())
		resp, err := outbound.Do(req)
		if err != nil {
			// retry
			<-time.After(15 * time.Second)
			resp, err = outbound.Do(req)
			if err != nil {
				return c.Reply(ø(errorCue, err))
			}
		}
		defer resp.Body.Close()
		var payload struct {
			Status     string `json:"status"`
			Hash       string `json:"hash"`
			URL        string `json:"url"`
			Filetype   string `json:"filetype"`
			DeleteCode string `json:"delete_code"`
			DeleteURL  string `json:"delete_url"`
		}
		err = json.NewDecoder(resp.Body).Decode(&payload)
		if err != nil {
			return c.Reply(ø(errorCue, err))
		}
		if payload.Status != "ok" {
			return c.Reply(ø(errorCue, payload))
		}
		message := "https://idiod.video/" + payload.URL
		og := c.Message().ReplyTo
		if og != nil {
			i := strings.Index(og.Text, "<")
			j := strings.Index(og.Text, ">")
			if j > 0 {
				message = og.Text[i+1:j] + ": " + message
			}
		}
		if err := broadcast(message); err != nil {
			// retry
			err = broadcast(message)
			if err != nil {
				return c.Reply(ø(errorCue, err))
			}
		}
		return nil
	})

	bot.Handle(tele.OnPinned, func(c tele.Context) error {
		return c.Delete()
	})

	go primo()
	bot.Start()
}

func load() {
	b, _ := ioutil.ReadFile(config)
	json.Unmarshal(b, this)
}

func save() {
	f, err := os.Create(config)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(this)
}

func login(username, password string) error {
	const path = "https://leprosorium.ru/ajax/auth/login/"
	form := map[string]string{
		"username":             username,
		"password":             password,
		"g-recaptcha-response": "",
	}
	pf := url.Values{}
	for k, v := range form {
		pf.Add(k, v)
	}
	resp, err := outbound.PostForm(path, pf)
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
	this.Username = username
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
	this.Cookies = outbound.Jar.Cookies(lepra)
	return nil
}

func poll() error {
	const path = "https://leprosorium.ru/ajax/chat/load/"
	form := map[string]string{
		"last_message_id": strconv.Itoa(this.LM),
		"csrf_token":      this.CSRF,
	}
	pf := url.Values{}
	for k, v := range form {
		pf.Add(k, v)
	}
	resp, err := outbound.PostForm(path, pf)
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
		this.LM = msg.ID
		this.Cunts[msg.User.ID] = msg.User

		author, text := msg.User.Login, msg.Body
		if author == this.Username {
			continue
		}

		send := func() error {
			if personal(msg.Body) {
				msg, err := bot.Send(this, ø(cue, author, text))
				if err == nil {
					bot.Pin(msg)
				}
				return err
			}
			_, err := bot.Send(this, ø(cue, author, text), tele.Silent)
			return err
		}
		if err := send(); err != nil {
			err = send()
			if err != nil {
				panic(err)
			}
		}
	}
	return nil
}

func broadcast(message string) error {
	const path = "https://leprosorium.ru/ajax/chat/add/"
	form := map[string]string{
		"last":       strconv.Itoa(this.LM),
		"csrf_token": this.CSRF,
		"body":       message,
	}
	pf := url.Values{}
	for k, v := range form {
		pf.Add(k, v)
	}
	resp, err := outbound.PostForm(path, pf)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func personal(text string) bool {
	for _, keyword := range append(this.Keywords, this.Username) {
		if strings.Index(text, keyword) >= 0 {
			return true
		}
	}
	return false
}
