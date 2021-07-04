package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	tele "gopkg.in/tucnak/telebot.v3"
)

const config = "/app/glavar.json"

var (
	bot *tele.Bot

	this = struct {
		Cunts map[int]*User  `json:"cunts"`
		LM    map[string]int `json:"lm"`
	}{
		Cunts: make(map[int]*User),
		LM:    make(map[string]int),
	}

	listening = map[string]bool{}
	// time since last error
	errt = time.Now().Add(-time.Hour)
	ø    = fmt.Sprintf
)

func main() {
	var err error

	load()
	save()
	defer save()

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

		tid := c.Sender().ID
		u := this.Cunts[tid]
		if u == nil {
			u = new(User)
			u.T = c.Sender()
			u.Login = args[0]
		}
		if err := u.login(args[1]); err != nil {
			return c.Reply(err.Error())
		}
		this.Cunts[tid] = u
		save()

		if !listening[u.Subsite] {
			err = u.primo(u.Subsite)
			if err != nil {
				return c.Reply(ø(errorCue, err))
			}
			listening[u.Subsite] = true
		}
		return nil
	})

	bot.Handle("/keywords", func(c tele.Context) error {
		if len(c.Args()) == 0 {
			return c.Reply(keywordIntroCue)
		}
		u, err := getuser(c, welcomeCue)
		if err != nil {
			return err
		}
		u.Keywords = c.Args()
		save()
		return c.Reply(ø(keywordsCue, u.Keywords))
	})

	bot.Handle("/subsite", func(c tele.Context) error {
		u, err := getuser(c, welcomeCue)
		if err != nil {
			return err
		}
		if len(c.Args()) != 1 || c.Args()[0] == "" {
			if u.Subsite == "" {
				return c.Reply(ø(subsiteIntroCue, "главной"))
			}
			return c.Reply(ø(subsiteIntroCue, u.Subsite))
		}

		var subsite string
		if len(c.Args()) == 1 {
			subsite = c.Args()[0]
		}
		if subsite == "!" {
			subsite = ""
		}
		if !listening[subsite] {
			err = u.primo(subsite)
			if err != nil {
				return c.Reply(ø(errorCue, err))
			}
			listening[subsite] = true
		}
		u.Subsite = subsite
		save()
		if u.Subsite == "" {
			return c.Reply(ø(subsiteChangedCue, "главную"))
		}
		return c.Reply(ø(subsiteChangedCue, u.Subsite))
	})

	bot.Handle(tele.OnText, func(c tele.Context) error {
		const bufsize = 4 * 255 // utf8 * limit

		var b bytes.Buffer
		b.Grow(bufsize)

		u, err := getuser(c, welcomeCue)
		if err != nil {
			return err
		}

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
				if err := u.broadcast(b.String()); err != nil {
					// retry
					err = u.broadcast(b.String())
					if err != nil {
						return c.Reply(ø(errorCue, err))
					}
				}
				b.Reset()
			}
		}
		if b.Len() > 0 {
			if err := u.broadcast(b.String()); err != nil {
				// retry
				err = u.broadcast(b.String())
				if err != nil {
					return c.Reply(ø(errorCue, err))
				}
			}
		}
		return nil
	})

	bot.Handle(tele.OnPhoto, func(c tele.Context) error {
		u, err := getuser(c, welcomeCue)
		if err != nil {
			return err
		}

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
		resp, err := u.outbound().Do(req)
		if err != nil {
			// retry
			<-time.After(15 * time.Second)
			resp, err = u.outbound().Do(req)
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
		if err := u.broadcast(message); err != nil {
			// retry
			err = u.broadcast(message)
			if err != nil {
				return c.Reply(ø(errorCue, err))
			}
		}
		return nil
	})

	bot.Handle(tele.OnPinned, func(c tele.Context) error {
		return c.Delete()
	})

	for _, u := range this.Cunts {
		if !listening[u.Subsite] && u.logged() {
			err = u.primo(u.Subsite)
			if err != nil {
				bot.Send(u.T, ø(errorCue, err)+" ["+u.Subsite+"]")
			}
			listening[u.Subsite] = true
		}
	}

	bot.Start()
}

func getuser(c tele.Context, cuecustom string) (*User, error) {
	tid := c.Sender().ID
	u, ok := this.Cunts[tid]
	if !ok || !u.logged() {
		return nil, c.Reply(cuecustom)
	}
	return u, nil
}

func load() {
	b, _ := ioutil.ReadFile(config)
	json.Unmarshal(b, &this)
}

func save() {
	f, err := os.Create(config)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(&this)
}
