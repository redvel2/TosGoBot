package main

import (
	"log"
	"gopkg.in/telegram-bot-api.v4"
	"github.com/go-pg/pg"
	"regexp"
	"fmt"
	"io/ioutil"
	"os"
)

var messageTemplate, _ = ioutil.ReadFile("message_template.html")
var helpTemplate, _ = ioutil.ReadFile("help_template.html")
var session = pg.Connect(&pg.Options{
      User: "azure",
      Password: "123454674",
      Database: "tos",
      Addr: "127.0.0.1:5432",
   })

var re = regexp.MustCompile(`^/([a-zA-z]+)\s*(.*)$`)

type Card struct{
	Id int
	Card_id string
	Name string
	Attribute string
	Rarity int
	Cost int
	Race string
	Series string
	MaxExp int
	Max_hp int
	Max_attk int
	Max_rec int
	TotalStats int
	WikiLink string
	PreviewLink string
}

func (card Card) String() string{
	return fmt.Sprintf("Id: %v\nName: %v\nMore info:\n%v", card.Card_id, card.Name, card.WikiLink)
}

type Command struct {
	raw_text string
	message string 
}

func (command *Command) IsValid() bool {
	return re.MatchString(command.raw_text)
}

func (command *Command) GetErrorMessage() string {
	return ""
}

func (command *Command) ShowCardInfo(card_id string) string {
	card := Card{}
	err := session.Model(&card).Where("card_id = ?", card_id).Limit(1).Select()
	if err != nil {
		fmt.Println(err)
		return command.GetErrorMessage()
	}
	res := fmt.Sprintf(string(messageTemplate), card.Name, card.Attribute, card.Card_id, card.Cost, card.Race, card.Series, card.Rarity, card.Max_hp, card.Max_attk, card.Max_rec, card.TotalStats, card.PreviewLink, card.WikiLink, card.Name)
	command.message = res
	return res
}

func (command *Command) Help() string{
	return string(helpTemplate)
}

func (command *Command) Report(s string) string {
	out, oerr := os.OpenFile("report.log", os.O_APPEND|os.O_WRONLY, 0600)//.Create("parsed.csv")
	if oerr != nil {
		return ""
	}
	defer out.Close()
	out.WriteString(s)
	out.WriteString("\n")
	return ""
} 

func (command *Command) Run() string {
	if command.message != "" {
		return command.message
	}
	if !command.IsValid() {
		return command.GetErrorMessage()
	}
	matches := re.FindStringSubmatch(command.raw_text)
	switch {
		case (matches[1] == "show" || matches[1] == "s") && len(matches)==3:
			return command.ShowCardInfo(matches[2])
		case matches[1] == "help":
			return command.Help()
		case matches[1] == "report" && len(matches) == 3:
			return command.Report(matches[2])
		default:
			return command.GetErrorMessage()
	}
}

func main() {
	token := "540153532:AAFfwWnRlqxINhjCWNz8s6S0rBRS3Cm7sOU"
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Panic(err)
	}
	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 10

	updates, err := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}
		log.Printf("[%s] %q", update.Message.From.UserName, update.Message.Text)
		command := Command{raw_text: update.Message.Text}
		resp := command.Run()
		if resp != ""{
			fmt.Println(resp)
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, resp)
			msg.ParseMode = "HTML"
			bot.Send(msg)
		}
	}
}
