package main

import (
	"fmt"
	tele "gopkg.in/tucnak/telebot.v3"
)

func main() {
	fmt.Println("vim-go")

	tele.NewBot(tele.Settings{})
}
