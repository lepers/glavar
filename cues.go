package main

const cue = `<pre>&lt;%s&gt;</pre> %s`
const startCue = "Главарь в здании. Для авторизации используй <code>/login Bruja_ mamkuebal</code> где Bruja_ это твой никнейм на лепре, а mamkuebal это твой пароль."
const nostartCue = "Ты уже залогинен. /logout"
const naxyuCue = "Иди нахуй тогда."
const welcomeCue = "Добро пожаловать. /logout"
const logoutCue = "Прощай! /login"
const errorCue = "❗️ %v"
const keywordIntroCue = `Ключевые слова

Если сообщение содержит одно из ключевых слов, то оно придет к тебе с уведомлением, а не "втихую".

Действует ограничение до 20 слов, не более 24 символов каждое.

<code>/keywords брухля брюха бружа брухжа</code>`
const keywordsCue = "Ключевые слова: %v"
const subsiteIntroCue = `Сейчас ты читаешь чердак <b>%s</b>.

Просто передай название интересующей тебя подлепры и читай ее чердак!

<code>/subsite shovinist</code>
<code>/subsite глагне</code> чтобы переключиться обратно на главную.
`
const subsiteChangedCue = "🛎 <b>%s</b>"
const ratelimitCue = "⚠️  Придержи свое траханье!"
const ignoringCue = `🤬 Функция игнора

Ответь на сообщение этого адского негодяя командой /ignore и он пропадет с твоих глаз на время продолжительностью до 24 часов!`
