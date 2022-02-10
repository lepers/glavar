package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bluele/gcache"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v4"
	"github.com/pkg/errors"
	"github.com/sethvargo/go-retry"

	tele "gopkg.in/telebot.v3"
)

const (
	// poll time
	polly = 5 * time.Second
	// max messaging frequency
	spitrate = 500 * time.Millisecond
	// nopreview, noshow limit
	rateWindow = 10 * time.Minute
	// max ignore time
	bannWindow = 24 * time.Hour
)

var (
	bot *tele.Bot

	this = struct {
		Users  map[int64]*User   `json:"cunts"`
		Models map[string]*Model `json:"models"`
		Lm     map[string]int    `json:"lm"` // last message
	}{
		Users:  make(map[int64]*User),
		Models: make(map[string]*Model),
		Lm:     make(map[string]int),
	}

	// listening[subsite name] is true when actively polling
	listening = map[string]bool{}
	// rate limiting
	rates = gcache.New(2000).LRU().Build()
	// polling queue
	pollq = make(chan string, 1)
	// logging queue
	logiq = make(chan Message, 100)
	// setup finished
	setup = make(chan struct{}, 1)

	ø       = fmt.Sprintf
	urlrx   = regexp.MustCompile(`[-a-zA-Z0-9@:%._\+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b([-a-zA-Z0-9()@:%_\+.~#?&//=]*)`)
	linkrx  = regexp.MustCompile(`<a href="([^<>]*)">([^<>]*)</a>`)
	expired = gcache.KeyNotFoundError

	ErrNotLogged    = errors.New("вы не авторизованы")
	ErrNotFound     = errors.New("подсайт не найден")
	ErrForbidden    = errors.New("вход воспрещен")
	ErrVoiceTooLong = errors.New("голосовое длиннее 30 сек")
	ErrOutboundSpam = errors.New("больше трех одинаковых сообщений")

	btnOK   = (*tele.ReplyMarkup)(nil).Data("👍", "model_ok")
	btnMore = (*tele.ReplyMarkup)(nil).Data("✍️", "model_more")

	datadir = os.Getenv("BOT_HOME")
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
			// double polly! classic!
			<-time.After(2 * polly)
			if !listening[subsite] {
				return c.Reply(ø(errorCue, ErrNotFound))
			}
		}
		u.Subsite = subsite
		save()
		if subsite == "" {
			subsite = "главная"
		}
		return c.Reply(ø(subsiteChangedCue, subsite))
	})
	bot.Handle("/ignore", func(c tele.Context) error {
		u, err := getuser(c)
		if err != nil {
			return c.Reply(welcomeCue)
		}

		target, _, ok := decouple(c.Message().ReplyTo)
		if !ok {
			return c.Reply(ignoringCue)
		}

		h := H(u.Login, target)
		t := rand.Intn(int(bannWindow))
		rates.SetWithExpire(h, true, time.Duration(t))
		return c.Reply("👍")
	})
	bot.Handle("/login_model", func(c tele.Context) error {
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
	bot.Handle("/bu", func(c tele.Context) error {
		return masquerade(c, "bukofka")
	})
	bot.Handle("/chl", func(c tele.Context) error {
		return masquerade(c, "chlenix")
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

		bot.Edit(cb.Message, strings.Join(res, "\n"), feedpicker(fid))
		return c.Respond()
	})
	bot.Handle(tele.OnText, func(c tele.Context) error {
		u, err := getuser(c)
		if err != nil {
			return c.Reply(welcomeCue)
		}

		text := c.Text()
		if strings.HasPrefix(text, "/") {
			return c.Reply("🚬")
		}

		target, _, ok := decouple(c.Message().ReplyTo)

		for i, line := range strings.Split(text, "\n") {
			if i == 0 && ok {
				line = target + ": " + line
			}
			if err := u.broadcast(line); err != nil {
				return c.Reply(ø(errorCue, err))
			}
			<-time.After(spitrate)
		}
		return nil
	})
	bot.Handle(tele.OnPhoto, media)
	bot.Handle(tele.OnVideo, media)
	bot.Handle(tele.OnAnimation, media)
	bot.Handle(tele.OnPinned, func(c tele.Context) error {
		return c.Delete()
	})

	// primo cannon in action!
	go func() {
		for subsite := range pollq {
			go primo(subsite)
		}
	}()

	// start listening to each and every subsite
	uniq := map[string]struct{}{}
	for i := range this.Users {
		s := this.Users[i].Subsite
		if _, ok := uniq[s]; ok {
			continue
		}
		pollq <- s
		uniq[s] = struct{}{}
	}

	const insert = "INSERT INTO logec (ts, seller, buyer, echo) VALUES ($1, $2, substring($3 FROM '^([a-zA-Z0-9_]+): '), $4)"
	go func() {
		type log struct {
			T time.Time
			U string
			B string
		}

		var (
			// disk backup (no lost messages)
			path    = datadir + "/backup.log"
			ms      = int64(time.Millisecond)
			pause   = time.Hour
			flags   = os.O_APPEND | os.O_WRONLY | os.O_CREATE
			rubicon = time.Now().Add(pause)
			bg      = context.Background()

			i, lag int64
		)

		db := makedb()
		for m := range logiq {
			i++
			if m.Created != lag {
				i = 0
				lag = m.Created
			}

			if db == nil && rubicon.Before(time.Now()) {
				db = makedb()
				rubicon = time.Now().Add(pause)
			}

			ts := time.Unix(m.Created, i*ms)
			user, body := m.User.Login, m.Body

			// temporarily writing to disk
			if db == nil {
				f, _ := os.OpenFile(path, flags, 0644)
				json.NewEncoder(f).Encode(&log{ts, user, body})
				f.Close()
				continue
			}

			var b pgx.Batch

			if f, err := os.Open(path); err == nil {
				r := json.NewDecoder(f)
				for x := new(log); err != nil; {
					err = r.Decode(x)
					b.Queue(insert, x.T, x.U, x.B, x.B)
				}
				f.Close()
				os.Remove(path)
			}

			b.Queue(insert, ts, user, body, body)

			var bo = backoff(10*time.Millisecond, 5*time.Second)
			retry.Do(bg, bo, func(c context.Context) error {
				err := db.SendBatch(c, &b).Close()
				return retry.RetryableError(err)
			})
		}
	}()

	setup <- struct{}{}
	bot.Start()
}

var replyFeeds = map[string]replyFeed{}

func masquerade(c tele.Context, model string) error {
	u, err := getuser(c)
	if err != nil {
		return c.Reply("😖")
	}

	sender, prompt, ok := decouple(c.Message().ReplyTo)
	if !ok {
		return c.Reply("🙈")
	}

	m := Model{User: u, Name: model}
	// generate result lines
	res := m.feed(sender, prompt)
	// make feed id
	fid := uuid.NewString()
	// memorize
	replyFeeds[fid] = replyFeed{sender, model, prompt, res, false}
	// put into a single message
	preview := strings.Join(res, "\n")

	return c.Reply(preview, feedpicker(fid))
}

func feedpicker(fid string) *tele.ReplyMarkup {
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
	var bo = backoff(time.Second, 15*time.Minute)

	err := retry.Do(nil, bo, func(_ context.Context) error {
		var principal error
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
				principal = err
			}
		}
		if principal != nil {
			return retry.RetryableError(principal)
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
		return "animation.mp4", r
	default:
		return "not_supported", nil
	}
}

func media(c tele.Context) error {
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

	sender, _, ok := decouple(c.Message().ReplyTo)
	if ok {
		message = sender + ": " + message
	}
	if err := u.broadcast(message); err != nil {
		return c.Reply(ø(errorCue, err))
	}
	return nil
}
