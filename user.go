package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pkg/errors"
	"github.com/sethvargo/go-retry"

	tele "gopkg.in/telebot.v3"
)

type User struct {
	T *tele.User `json:"tele"`

	ID      int    `json:"id"`
	Active  int    `json:"active"`
	Deleted int    `json:"deleted"`
	Karma   int    `json:"karma"`
	Login   string `json:"login"`
	City    string `json:"city"`
	Gender  string `json:"gender"`
	Csrf    string `json:"csrf_token"`
	Subsite string `json:"subsite"`

	Keywords []string `json:"keys"`

	Jar []*http.Cookie `json:"jar"`
}

type Message struct {
	User    User   `json:"user"`
	ID      int    `json:"id"`
	Body    string `json:"body"`
	Created int64  `json:"created"`
}

func (m Message) String() string {
	return ø("%d <%s> %s", m.ID, m.User.Login, m.Body)
}

func (u *User) logged() bool {
	return u.Csrf != ""
}

func (u *User) outbound() *http.Client {
	var lepraURL, _ = url.Parse("https://leprosorium.ru")

	client := &http.Client{}
	client.Jar, _ = cookiejar.New(nil)
	client.Jar.SetCookies(lepraURL, u.Jar)
	return client
}

func (u *User) api(path string, subsite string) string {
	if subsite != "" {
		subsite += "."
	}
	return "https://" + subsite + "leprosorium.ru" + path
}

func (u *User) login(password string) error {
	path := u.api("/ajax/auth/login/", "")

	form := map[string]string{
		"username": u.Login,
		"password": password,

		"g-recaptcha-response": "",
	}
	pf := url.Values{}
	for k, v := range form {
		pf.Add(k, v)
	}
	resp, err := (&http.Client{}).PostForm(path, pf)
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
	u.Csrf = csrf.Token
	u.Jar = resp.Cookies()
	return nil
}

func (u *User) broadcast(message string) error {
	h, rate := H(message, u.Login), 1
	if v, err := rates.Get(h); err != expired {
		rate += v.(int)
	}
	rates.SetWithExpire(h, rate, rateWindow)
	if rate > 3 {
		return ErrOutboundSpam
	}
	if rate > 2 && u.T != nil {
		bot.Send(u.T, ratelimitCue)
	}

	path := u.api("/ajax/chat/add/", u.Subsite)
	form := map[string]string{
		"last":       strconv.Itoa(this.Lm[u.Subsite]),
		"csrf_token": u.Csrf,
		"body":       message,
	}
	pf := url.Values{}
	for k, v := range form {
		pf.Add(k, v)
	}

	client := u.outbound()
	resp, err := client.PostForm(path, pf)
	if err != nil {
		// best intentions
		go func() {
			bo := backoff(100*time.Millisecond, time.Minute)
			retry.Do(bg, bo, func(_ context.Context) error {
				resp, err := client.PostForm(path, pf)
				if err != nil {
					return retry.RetryableError(err)
				}
				return resp.Body.Close()
			})
		}()
		return err
	}

	return resp.Body.Close()
}

func (u *User) personal(text string) bool {
	for _, keyword := range append(u.Keywords, u.Login) {
		if strings.Index(text, keyword) >= 0 {
			return true
		}
	}
	return false
}

func (u *User) primo(subsite string) error {
	if !listening[subsite] && !u.subsiteExists(subsite) {
		return ErrNotFound
	}

	err := u.poll(subsite)
	if err != nil {
		return err
	}

	go func() {
		for range time.NewTicker(polly).C {
			err = u.poll(subsite)
			if err != nil && err != io.EOF {
				listening[subsite] = false
				pollq <- subsite
				break
			}
		}
	}()
	listening[subsite] = true
	return nil
}

func (u *User) poll(subsite string) error {
	defer save()

	path := u.api("/ajax/chat/load/", subsite)
	form := map[string]string{
		"last_message_id": strconv.Itoa(this.Lm[subsite]),
		"csrf_token":      u.Csrf,
	}
	pf := url.Values{}
	for k, v := range form {
		pf.Add(k, v)
	}
	resp, err := u.outbound().PostForm(path, pf)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var schema struct {
		Messages []Message `json:"messages"`
		Err      []struct {
			Code string `json:"code"`
		} `json:"errors,omitempty"`
	}

	err = json.NewDecoder(resp.Body).Decode(&schema)
	if err != nil {
		return errors.Wrap(err, "updates could not be decoded")
	}

	if schema.Err != nil {
		code := schema.Err[0].Code
		if code == "not_authorized" {
			return ErrForbidden
		}
		return errors.New("exotic api error: " + code)
	}

	for _, msg := range schema.Messages {
		// let's push things forward
		this.Lm[subsite] = msg.ID

		var (
			sender = msg.User.Login
			body   = msg.Body
		)

		body = strings.ReplaceAll(body, "\n", " ")
		body = strings.ReplaceAll(body, "\u00a0", " ")
		body = linkrx.ReplaceAllString(body, "$1")
		body = strings.TrimSpace(body)
		if utf8.RuneCountInString(body) == 0 {
			continue
		}
		msg.Body = body

		h, rate := H(sender, body), 1
		if v, err := rates.Get(h); err != expired {
			rate += v.(int)
		}
		rates.SetWithExpire(h, rate, rateWindow)
		if rate > 3 {
			continue
		}

		// and now the entities
		var noshow, nopreview bool
		for _, url := range urlrx.FindAllString(body, -1) {
			h, rate := H(url), 1
			if v, err := rates.Get(url); err != expired {
				rate += v.(int)
			}
			rates.SetWithExpire(h, rate, rateWindow)
			if rate > 1 {
				nopreview = true
			}
			if rate > 3 {
				noshow = true
				break
			}
		}

		if subsite == "" {
			logiq <- msg

			rand.Seed(time.Now().Unix())
			for _, model := range this.Models {
				// don't engage with itself
				if model.User.Login == sender {
					continue
				}
				// only 20% of messages re-engage other models
				if ismodel(sender) && rand.Float32() > 0.2 {
					continue
				}
				// final filter: 5% engagement for nonpersonal
				if !model.personal(body) || rand.Float32() > 0.05 {
					continue
				}
				model.feed(sender, body)
			}
		}

		if noshow {
			continue
		}

		for id, u := range this.Users {
			if u.Login == sender || u.Subsite != subsite {
				continue
			}

			h := H(u.Login, sender)
			if _, err = rates.Get(h); err != expired {
				continue
			}

			personal := u.personal(body)

			opts := &tele.SendOptions{
				DisableWebPagePreview: nopreview,
				DisableNotification:   !personal,
			}

			body := ø(cue, sender, body)
			msg, err := bot.Send(u.T, body, opts)
			if err != nil {
				if err == tele.ErrBlockedByUser {
					delete(this.Users, id)
					continue
				}
				// retry
				msg, err = bot.Send(u.T, body, opts)
				if err != nil {
					continue
				}
			}
			if personal {
				bot.Pin(msg)
			}
		}
	}

	return nil
}

func (u *User) upload(c tele.Context, f string, r io.Reader) (string, error) {
	const path = "https://idiod.video/api/upload.php"

	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, _ := w.CreateFormFile("file", f)
	if _, err := io.Copy(fw, r); err != nil {
		return "", c.Reply(ø(errorCue, err))
	}
	w.Close()
	req, err := http.NewRequest("POST", path, &b)
	if err != nil {
		return "", c.Reply(ø(errorCue, err))
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := u.outbound().Do(req)
	if err != nil {
		// retry
		<-time.After(15 * time.Second)
		resp, err = u.outbound().Do(req)
		if err != nil {
			return "", c.Reply(ø(errorCue, err))
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
		return "", c.Reply(ø(errorCue, err))
	}
	if payload.Status != "ok" {
		return "", c.Reply(ø(errorCue, payload))
	}
	return "https://idiod.video/" + payload.URL, nil
}

func (u *User) subsiteExists(subsite string) bool {
	if subsite == "" {
		return true
	}
	path := "https://" + subsite + ".leprosorium.ru"
	client := u.outbound()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Get(path)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}
