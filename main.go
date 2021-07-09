package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bluele/gcache"
	"github.com/pkg/errors"
	"github.com/sethvargo/go-retry"
	tele "gopkg.in/tucnak/telebot.v3"
)

var (
	config   = os.Getenv("BOT_CONFIG")
	yandexId = os.Getenv("YANDEX_ID")
	yandex   = ""

	bot *tele.Bot

	this = struct {
		Cunts map[int]*User  `json:"cunts"` // lepers
		LM    map[string]int `json:"lm"`    // last message
	}{
		Cunts: make(map[int]*User),
		LM:    make(map[string]int),
	}

	// listening[subsite name] is true when actively polling
	listening = map[string]bool{}

	// base rate limiting
	rateWindow = 10 * time.Minute
	rates      = gcache.New(2000).LRU().Build()
	black      = gcache.New(1000).LRU().Build()

	ø = fmt.Sprintf

	// polling queue
	pollq = make(chan string, 1)

	ErrNotLogged    = errors.New("вы не авторизованы")
	ErrNotFound     = errors.New("подсайт не найден")
	ErrForbidden    = errors.New("вход воспрещен")
	ErrVoiceTooLong = errors.New("голосовое длиннее 30 сек")
)

func main() {
	if config == "" {
		config = "/app/glavar.json"
	}
	load()
	save()
	defer save()

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
			pollq <- u.Subsite
		}
		return c.Delete()
	})

	bot.Handle("/logout", func(c tele.Context) error {
		u, err := getuser(c)
		if err != nil {
			return c.Reply(welcomeCue)
		}

		delete(this.Cunts, u.T.ID)
		save()
		return c.Reply(logoutCue)
	})

	bot.Handle("/keywords", func(c tele.Context) error {
		args := c.Args()
		if len(args) == 0 {
			return c.Reply(keywordIntroCue)
		}
		u, err := getuser(c)
		if err != nil {
			return c.Reply(welcomeCue)
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
		u, err := getuser(c)
		if err != nil {
			return c.Reply(welcomeCue)
		}
		if len(c.Args()) != 1 {
			if u.Subsite == "" {
				return c.Reply(ø(subsiteIntroCue, "главной"))
			}
			return c.Reply(ø(subsiteIntroCue, u.Subsite))
		}

		var subsite string
		if len(c.Args()) == 1 {
			subsite = c.Args()[0]
		}
		if subsite == "глагне" || subsite == "главная" {
			subsite = ""
		}
		if !listening[subsite] {
			pollq <- subsite
			<-time.After(3 * time.Second)
			if !listening[subsite] {
				return c.Reply(ø(errorCue, ErrNotFound))
			}
		}
		u.Subsite = subsite
		save()
		if subsite == "" {
			return c.Reply(ø(subsiteChangedCue, "главную"))
		}
		return c.Reply(ø(subsiteChangedCue, subsite))
	})

	bot.Handle("/ignore", func(c tele.Context) error {
		u, err := getuser(c)
		if err != nil {
			return c.Reply(welcomeCue)
		}

		og := c.Message().ReplyTo
		if og == nil {
			return c.Reply(ignoringCue)
		}

		rand.Seed(time.Now().Unix())

		i := strings.Index(og.Text, "<")
		j := strings.Index(og.Text, ">")
		if i < 0 || j < 0 {
			return nil
		}
		k := u.Login + "~" + og.Text[i+1:j]
		t := rand.Intn(int(24 * time.Hour))
		black.SetWithExpire(k, 1, time.Duration(t))
		return c.Reply("👍")
	})

	bot.Handle(tele.OnText, func(c tele.Context) error {
		const bufsize = 4 * 255 // utf8 * limit

		var b bytes.Buffer
		b.Grow(bufsize)

		u, err := getuser(c)
		if err != nil {
			return c.Reply(welcomeCue)
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
					return c.Reply(ø(errorCue, err))
				}
				b.Reset()
			}
		}
		if b.Len() > 0 {
			if err := u.broadcast(b.String()); err != nil {
				return c.Reply(ø(errorCue, err))
			}
		}
		return nil
	})

	bot.Handle(tele.OnPhoto, handleMedia)
	bot.Handle(tele.OnVideo, handleMedia)
	bot.Handle(tele.OnAnimation, handleMedia)
	bot.Handle(tele.OnVoice, func(c tele.Context) error {
		if yandex == "" {
			return c.Delete()
		}

		u, err := getuser(c)
		if err != nil {
			return c.Reply(welcomeCue)
		}

		rate, err := rates.Get("@"+u.Login)
		if err == gcache.KeyNotFoundError {
			rates.SetWithExpire("@"+u.Login, 0, rateWindow)
			rate = 0
		}
		n := rate.(int)
		if n > 10 {
			return c.Reply(ratelimitCue)
		}

		voc := c.Message().Voice
		if voc.Duration > 30 {
			return c.Reply(ø(errorCue, ErrVoiceTooLong))
		}

		r, err := bot.File(&voc.File)
		if err != nil {
			return c.Reply(ø(errorCue, err))
		}

		path := "https://stt.api.cloud.yandex.net/speech/v1/stt:recognize?topic=general:rc&folderId=" + yandexId
		req, _ := http.NewRequest("POST", path, r)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Add("Authorization", "Bearer "+yandex)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return c.Reply(ø(errorCue, err))
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		data := struct {
			Result string `json:"result"`
		}{}
		err = json.Unmarshal(b, &data)
		if err != nil {
			return c.Reply(ø(errorCue, err))
		}

		message := data.Result

		og := c.Message().ReplyTo
		if og != nil {
			i := strings.Index(og.Text, "<")
			j := strings.Index(og.Text, ">")
			if j > 0 {
				message = og.Text[i+1:j] + ": " + message
			}
		}

		message = "🎙 " + message
		err = u.broadcast(message)
		if err != nil {
			return c.Reply(ø(errorCue, err))
		}

		n++
		rates.Set("@"+u.Login, n)
		return c.Reply(message)
	})
	bot.Handle(tele.OnPinned, func(c tele.Context) error {
		return c.Delete()
	})

	go func() {
		for subsite := range pollq {
			go primo(subsite)
		}
	}()

	go func() {
		subsites := map[string]bool{}
		for _, u := range this.Cunts {
			subsites[u.Subsite] = true
		}
		for subsite := range subsites {
			pollq <- subsite
		}
	}()

	go func() {
		api := os.Getenv("YANDEX_API")
		if api == "" {
			return
		}
		const path = "https://iam.api.cloud.yandex.net/iam/v1/tokens"
		for {
			data := `{"yandexPassportOauthToken":"` + api + `"}`
			r := strings.NewReader(data)
			resp, err := http.Post(path, "application/json", r)
			if err != nil {
				fmt.Println("yandex:", err)
				continue
			}

			token := struct {
				S string `json:"iamToken"`
			}{}
			err = json.NewDecoder(resp.Body).Decode(&token)
			resp.Body.Close()
			if err != nil {
				fmt.Println("yandex:", err)
				continue
			}
			yandex = token.S

			<-time.After(time.Hour)
		}
	}()

	bot.Start()
}

func primo(subsite string) {
	var (
		c = context.Background()

		fib, _ = retry.NewFibonacci(1 * time.Second)
		dt     = retry.WithMaxDuration(15*time.Minute, fib)
	)
	err := retry.Do(c, dt, func(c context.Context) error {
		var OG error

		for _, u := range this.Cunts {
			if !u.logged() {
				continue
			}

			err := u.primo(subsite)
			switch err {
			case ErrNotFound:
			case ErrForbidden:
			case nil:
				return nil
			default:
				OG = err
			}
		}
		if OG != nil {
			return retry.RetryableError(OG)
		}
		return nil
	})

	if err != nil {
		fmt.Println(err)
		panic("primo(" + subsite + ") failed")
	}
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
	u, err := getuser(c)
	if err != nil {
		return c.Reply(welcomeCue)
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
		return c.Reply(ø(errorCue, err))
	}
	return nil
}

func getuser(c tele.Context) (*User, error) {
	tid := c.Sender().ID
	u, ok := this.Cunts[tid]
	if !ok || !u.logged() {
		return nil, ErrNotLogged
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
	enc := json.NewEncoder(f)
	enc.SetIndent("", "\t")
	enc.Encode(&this)
}
