package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bluele/gcache"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/sethvargo/go-retry"
	tele "gopkg.in/tucnak/telebot.v3"
)

var (
	datadir = os.Getenv("BOT_HOME")

	bot *tele.Bot

	this = struct {
		Users  map[int]*User     `json:"cunts"`
		Models map[string]*Model `json:"models"`
		Lm     map[string]int    `json:"lm"` // last message
	}{
		Users:  make(map[int]*User),
		Models: make(map[string]*Model),
		Lm:     make(map[string]int),
	}

	// listening[subsite name] is true when actively polling
	listening = map[string]bool{}

	// base rate limiting
	rateWindow = 10 * time.Minute
	rates      = gcache.New(2000).LRU().Build()
	black      = gcache.New(1000).LRU().Build()

	ø         = fmt.Sprintf
	simpleURL = regexp.MustCompile(`[-a-zA-Z0-9@:%._\+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b([-a-zA-Z0-9()@:%_\+.~#?&//=]*)`)

	// polling queue
	pollq = make(chan string, 1)
	// setup finished
	setup = make(chan struct{}, 1)

	ErrNotLogged    = errors.New("вы не авторизованы")
	ErrNotFound     = errors.New("подсайт не найден")
	ErrForbidden    = errors.New("вход воспрещен")
	ErrVoiceTooLong = errors.New("голосовое длиннее 30 сек")

	btnOK   = (*tele.ReplyMarkup)(nil).Data("👍", "model_ok")
	btnMore = (*tele.ReplyMarkup)(nil).Data("✍️", "model_more")
)

func main() {
	if datadir == "" {
		datadir = "."
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
		u := this.Users[tid]
		if u == nil {
			u = new(User)
			u.T = c.Sender()
			u.Login = args[0]
		}
		if err := u.login(args[1]); err != nil {
			return c.Reply(err.Error())
		}
		this.Users[tid] = u
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

		delete(this.Users, u.T.ID)
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
	bot.Handle("/model_login", func(c tele.Context) error {
		if c.Sender().Username != "tucnak" {
			return nil
		}
		args := c.Args()
		if len(args) != 3 {
			return c.Reply("username password port")
		}

		u := new(User)
		u.Login = args[0]
		if err := u.login(args[1]); err != nil {
			return c.Reply(err.Error())
		}
		this.Models[u.Login] = &Model{
			User: u,
			Name: args[2],
		}
		save()
		return c.Delete()
	})
	bot.Handle("/model", func(c tele.Context) error {
		if c.Sender().Username != "tucnak" {
			return nil
		}
		args := c.Args()
		if len(args) != 2 {
			return c.Reply("username on/off")
		}

		login, status := args[0], false
		if args[1] == "on" {
			status = true
		}

		if mod, ok := this.Models[login]; ok {
			mod.Busy = status
		}

		save()
		return c.Reply("👍")
	})
	bot.Handle("/bu", func(c tele.Context) error {
		return sendAs(c, "bukofka")
	})
	bot.Handle("/chl", func(c tele.Context) error {
		return sendAs(c, "chlenix")
	})
	bot.Handle(&btnOK, func(c tele.Context) error {
		fid := c.Callback().Data
		f, ok := replyFeeds[fid]
		if !ok {
			c.Delete()
			return c.Send("🐔")
		}

		u, err := getuser(c)
		if err != nil {
			return c.Reply("😖")
		}
		m := &Model{u, f.bot, false}
		go m.deliver(f.login, f.result)

		delete(replyFeeds, fid)
		bot.Delete(c.Callback().Message)
		return c.Respond()
	})
	bot.Handle(&btnMore, func(c tele.Context) error {
		cb := c.Callback()
		fid := cb.Data
		f, ok := replyFeeds[fid]
		if !ok {
			c.Delete()
			return c.Send("🐔")
		}

		if f.busy {
			return c.Respond(&tele.CallbackResponse{Text: busyCue})
		}

		u, err := getuser(c)
		if err != nil {
			return c.Reply("😖")
		}

		f.busy = true
		replyFeeds[fid] = f
		c.Notify(tele.Typing)

		m := &Model{u, f.bot, false}
		res := m.feed(f.login, f.prompt)
		f.result = res
		f.busy = false
		replyFeeds[fid] = f

		bot.Edit(cb.Message, strings.Join(res, "\n"), modelMenu(fid))
		return c.Respond()
	})
	bot.Handle(tele.OnText, func(c tele.Context) error {
		u, err := getuser(c)
		if err != nil {
			return c.Reply(welcomeCue)
		}

		message := c.Text()
		if strings.HasPrefix(message, "/") {
			return c.Reply("🚬")
		}

		og, target := c.Message().ReplyTo, ""
		if og != nil {
			i := strings.Index(og.Text, "<")
			j := strings.Index(og.Text, ">")
			if j > 0 {
				target = og.Text[i+1 : j]
			}
		}

		for i, message := range strings.Split(message, "\n") {
			if i == 0 && target != "" {
				message = target + ": " + message
			}
			if err := u.broadcast(message); err != nil {
				return c.Reply(ø(errorCue, err))
			}
			<-time.After(500 * time.Millisecond)
		}
		return nil
	})
	bot.Handle(tele.OnPhoto, handleMedia)
	bot.Handle(tele.OnVideo, handleMedia)
	bot.Handle(tele.OnAnimation, handleMedia)
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
		for _, u := range this.Users {
			subsites[u.Subsite] = true
		}
		for subsite := range subsites {
			pollq <- subsite
		}
	}()

	setup <- struct{}{}
	bot.Start()
}

var replyFeeds = map[string]replyFeed{}

func sendAs(c tele.Context, name string) error {
	u, err := getuser(c)
	if err != nil {
		return c.Reply("😖")
	}

	og := c.Message().ReplyTo
	if og == nil {
		return c.Reply("🙈")
	}

	i := strings.Index(og.Text, "<")
	j := strings.Index(og.Text, ">")
	if j < 0 {
		return c.Reply("🧐")
	}

	author, prompt := og.Text[i+1:j], og.Text[j+2:]
	m := Model{
		User: u,
		Name: name,
	}
	res := m.feed(author, prompt)
	fid := uuid.NewString()
	replyFeeds[fid] = replyFeed{author, name, prompt, res, false}
	return c.Reply(strings.Join(res, "\n"), modelMenu(fid))
}

func modelMenu(fid string) *tele.ReplyMarkup {
	var (
		menu = &tele.ReplyMarkup{ResizeKeyboard: true}
		btn  = btnOK
		btm  = btnMore
	)
	btn.Data = fid
	btm.Data = fid
	menu.Inline(menu.Row(btn, btm))
	return menu
}

func primo(subsite string) {
	var (
		c = context.Background()

		fib, _ = retry.NewFibonacci(1 * time.Second)
		dt     = retry.WithMaxDuration(15*time.Minute, fib)
	)
	err := retry.Do(c, dt, func(c context.Context) error {
		var OG error

		for _, u := range this.Users {
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
