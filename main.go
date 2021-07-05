package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bluele/gcache"
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

	rateWindow = 10 * time.Minute
	rates      = gcache.New(1000).LRU().Build()
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
		args := c.Args()
		if len(args) == 0 {
			return c.Reply(keywordIntroCue)
		}
		u, err := getuser(c, welcomeCue)
		if err != nil {
			return err
		}

		k := make([]string, 0, 20)
		n := len(args)
		if n > 20 {
			n = 20
		}
		for i := 0; i < n; i++ {
			if utf8.RuneCountInString(args[i]) > 24 {
				continue
			}
			k = append(k, args[i])
		}
		u.Keywords = k
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

	bot.Handle(tele.OnPhoto, handleMedia)
	bot.Handle(tele.OnVideo, handleMedia)
	bot.Handle(tele.OnAnimation, handleMedia)
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

func mediaOf(msg *tele.Message) (string, io.Reader) {
	switch {
	case msg.Photo != nil:
		r, _ := bot.File(&msg.Photo.File)
		return "image.jpg", r
	case msg.Video != nil:
		r, _ := bot.File(&msg.Video.File)
		return "video.mp4", r
	case msg.Animation != nil:
		r, _ := bot.File(&msg.Animation.File)
		return "video.mp4", r
	}
	return "", nil
}

func handleMedia(c tele.Context) error {
	u, err := getuser(c, welcomeCue)
	if err != nil {
		return err
	}
	rf, r := mediaOf(c.Message())
	if r == nil {
		return c.Reply(ø(errorCue, "Unsupported media type."))
	}
	message, err := u.upload(c, rf, r)
	if err != nil {
		return err
	}
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
