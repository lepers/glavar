package main

type User struct {
	City    string `json:"city"`
	Deleted int    `json:"deleted"`
	Gender  string `json:"gender"`
	Karma   int    `json:"karma"`
	Login   string `json:"login"`
	Active  int    `json:"active"`
	ID      int    `json:"id"`
}

type Message struct {
	Body    string `json:"body"`
	Created int    `json:"created"`
	ID      int    `json:"id"`
	User    User   `json:"user"`
}
