package main

// cue(espondence)
const cue = `<pre>&lt;%s&gt;</pre> %s`
const startCue = "Главарь в здании. Для авторизации используй <code>/login Bruja_ mamkuebal</code> где Bruja_ это твой никнейм на лепре, а mamkuebal это твой пароль."
const nostartCue = "Ты уже залогинен. /logout"
const naxyuCue = "Иди нахуй тогда."
const welcomeCue = "Добро пожаловать. /logout"
const errorCue = "❗️ %v"
const keywordIntroCue = `Ключевые слова

Если сообщение содержит одно из ключевых слов, то оно придет к тебе с уведомлением, а не "втихую".

<code>/keywords брухля брюха бружа брухжа</code>`
const keywordsCue = "Ключевые слова: %v"
const subsiteIntroCue = `Сейчас ты читаешь чердак %s.

Просто передай название интересующей тебя подлепры и читай ее чердак!

<code>/subsite shovinist</code>
<code>/subsite !</code> чтобы переключиться обратно на главную.
`
const subsiteChangedCue = "🛎 Переключаюсь на %s..."
