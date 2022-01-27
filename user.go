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

	"github.com/bluele/gcache"
	"github.com/pkg/errors"
	"github.com/sethvargo/go-retry"
	tele "gopkg.in/tucnak/telebot.v3"
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
	Created int    `json:"created"`
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
	key := u.Login + "\n\n" + message
	value, err := rates.Get(key)
	if err == gcache.KeyNotFoundError {
		value = 0
		rates.SetWithExpire(key, 0, rateWindow)
	}
	rate := value.(int) + 1
	rates.Set(key, rate)
	if rate > 5 {
		_, err := bot.Send(u.T, ratelimitCue)
		return err
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
			c := context.Background()
			fib, _ := retry.NewFibonacci(100 * time.Millisecond)
			dt := retry.WithMaxDuration(1*time.Minute, fib)

			retry.Do(c, dt, func(c context.Context) error {
				resp, err := client.PostForm(path, pf)
				if err != nil {
					return retry.RetryableError(err)
				}
				return resp.Body.Close()
			})
		}()
		return err
	}
	resp.Body.Close()
	return nil
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
	go func(u *User, subsite string) {
		for range time.NewTicker(5 * time.Second).C {
			err = u.poll(subsite)
			if err != nil && err != io.EOF {
				listening[subsite] = false
				pollq <- subsite
				break
			}
		}
	}(u, subsite)
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

		Err []struct {
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
		this.Lm[subsite] = msg.ID
		body := strings.TrimSpace(msg.Body)
		if len(body) == 0 {
			continue
		}
		body = strings.ReplaceAll(body, "\n", " ")

		author := msg.User.Login

		// the message itself
		key := author + "\n" + body
		value, err := rates.Get(key)
		if err == gcache.KeyNotFoundError {
			value = 0
		}
		rate := value.(int) + 1
		rates.SetWithExpire(key, rate, rateWindow)
		if rate > 3 {
			continue
		}

		// and now its entities
		var noshow, nopreview bool
		urls := simpleURL.FindAllString(body, -1)
		for _, url := range urls {
			value, err := rates.Get(url)
			if err == gcache.KeyNotFoundError {
				value = 0
			}
			rate := value.(int) + 1
			rates.SetWithExpire(url, rate, rateWindow)
			if rate > 3 {
				nopreview = true
			}
			if rate > 5 {
				noshow = true
				break
			}
		}
		if noshow {
			continue
		}

		ismodel := func(name string) bool {
			for _, model := range this.Models {
				if model.User.Login == name {
					return true
				}
			}
			return false
		}

		// length := utf8.RuneCountInString(body)
		if subsite == "" {
			rand.Seed(time.Now().Unix())

			for _, model := range this.Models {
				modelname := model.User.Login
				if author == modelname {
					continue
				}

				if ismodel(author) && rand.Float32() > 0.2 {
					continue
				}

				// special case
				personal := false
				for _, name := range model.User.Keywords {
					if strings.Contains(body, name) {
						personal = true
						break
					}
				}
				if personal {
					model.feed(author, body)
					continue
				}

				if rand.Float32() > 0.05 {
					continue
				}
				model.feed(author, body)
			}
		}

		for id, u := range this.Users {
			if u.Login == author || u.Subsite != subsite {
				continue
			}
			_, err = black.Get(u.Login + "~" + author)
			if err != gcache.KeyNotFoundError {
				continue
			}

			personal := u.personal(body)

			opts := &tele.SendOptions{
				DisableWebPagePreview: nopreview,
				DisableNotification:   !personal,
			}

			body := ø(cue, author, body)
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
