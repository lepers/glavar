package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
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
	Body    string `json:"body"`
	Created int    `json:"created"`
	ID      int    `json:"id"`
	User    User   `json:"user"`
}

func (u *User) logged() bool {
	return u.Csrf != ""
}

var lepra, _ = url.Parse("https://leprosorium.ru")

func (u *User) outbound() *http.Client {
	client := &http.Client{}
	client.Jar, _ = cookiejar.New(nil)
	client.Jar.SetCookies(lepra, u.Jar)
	return client
}

func (u *User) api(path string) string {
	var subsite = u.Subsite
	if subsite != "" {
		subsite += "."
	}
	return "https://" + subsite + "leprosorium.ru" + path
}

func (u *User) login(password string) error {
	path := u.api("/ajax/auth/login/")

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
	path := u.api("/ajax/chat/add/")

	form := map[string]string{
		"last":       strconv.Itoa(this.LM[u.Subsite]),
		"csrf_token": u.Csrf,
		"body":       message,
	}
	pf := url.Values{}
	for k, v := range form {
		pf.Add(k, v)
	}

	resp, err := u.outbound().PostForm(path, pf)
	if err != nil {
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
	err := u.poll(subsite)
	if err != nil {
		return err
	}
	go func() {
		for range time.NewTicker(5 * time.Second).C {
			err = u.poll(subsite)
			if err != nil && err != io.EOF {
				if time.Now().Sub(errt) > 10*time.Minute {
					bot.Send(u.T, ø(errorCue, err)+" ["+subsite+"]")
					errt = time.Now()
				}
			}
		}
	}()
	return nil
}

func (u *User) poll(subsite string) error {
	path := u.api("/ajax/chat/load/")
	form := map[string]string{
		"last_message_id": strconv.Itoa(this.LM[subsite]),
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
	}
	err = json.NewDecoder(resp.Body).Decode(&schema)
	if err != nil {
		return errors.Wrap(err, "updates could not be decoded")
	}

	defer save()

	for _, msg := range schema.Messages {
		this.LM[subsite] = msg.ID

		author, text := msg.User.Login, msg.Body
		for _, u := range this.Cunts {
			if u.Login == author || u.Subsite != subsite {
				continue
			}
			if u.personal(msg.Body) {
				msg, err := bot.Send(u.T, ø(cue, author, text))
				if err != nil {
					// retry
					msg, err = bot.Send(u.T, ø(cue, author, text))
					if err != nil {
						return err
					}
				}
				bot.Pin(msg)
				break

			}
			_, err := bot.Send(u.T, ø(cue, author, text), tele.Silent)
			if err != nil {
				_, err = bot.Send(u.T, ø(cue, author, text), tele.Silent)
				if err != nil {
					return err
				}
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
