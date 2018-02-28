package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-pg/pg"
	"github.com/go-redis/redis"
	"gopkg.in/ini.v1"
	"gopkg.in/telegram-bot-api.v4"
)

var config, _ = ini.Load("config.ini")
var messageTemplate, _ = ioutil.ReadFile("message_template.html")
var miniMessageTemplate, _ = ioutil.ReadFile("message_template_min.html")
var helpTemplate, _ = ioutil.ReadFile("help_template.html")
var session = pg.Connect(&pg.Options{
	User:     config.Section("database").Key("user").Value(),
	Password: config.Section("database").Key("password").Value(),
	Database: config.Section("database").Key("name").Value(),
	Addr:     config.Section("database").Key("host").Value(),
})

var client = redis.NewClient(&redis.Options{
	Addr:     "localhost:6379",
	Password: "", // no password set
	DB:       0,  // use default DB
})

const REDIS_DEFAULT_TIMEOUT = 0

var InlineKeyboard = tgbotapi.NewInlineKeyboardMarkup(
	tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("1", "some_query"),
	),
	tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("2", "some_query2"),
	),
)

var re = regexp.MustCompile(`^/([a-zA-z]+)@tos_helper_bot\s*(.*)$`)
var re2 = regexp.MustCompile(`^/([a-zA-z]+)\s*(.*)$`)

var ConfirmRow = tgbotapi.NewInlineKeyboardRow(
	tgbotapi.NewInlineKeyboardButtonData("✅ Ок", "save"),
	tgbotapi.NewInlineKeyboardButtonData("🚫 Отменить", "cancel"),
)

const CARD_DISPLAY_MODE_NORMAL = 1
const CARD_DISPLAY_MODE_FULL = 2

type Skill struct {
	Id      int
	SkillId string
	Name    string
	Lv1cd   int
	Lvmaxcd int
	Effect  string
	Type    int
}

type Card struct {
	Id            int
	Card_id       string
	Name          string
	Attribute     string
	Rarity        int
	Cost          int
	Race          string
	Series        string
	MaxExp        int
	Max_hp        int
	Max_attk      int
	Max_rec       int
	TotalStats    int
	WikiLink      string
	PreviewLink   string
	ActiveSkillId int
	ActiveSkill   *Skill
	LeaderSkillId int
	LeaderSkill   *Skill
}

// func (card Card) String() string{
// 	return fmt.Sprintf("Id: %v\nName: %v\nMore info:\n%v", card.Card_id, card.Name, card.WikiLink)
// }

type Command struct {
	raw_text     string
	message      string
	commWord     string
	commParams   []string
	reply_markup interface{}
	tgRequest    *tgbotapi.Update
	postData     string
}

type InlineQueryCard struct {
	CardId string
	Name   string
}

type InlineQueryInfo struct {
	UserId      int
	DisplayMode int
	CardsList   []InlineQueryCard
}

func (command *Command) IsValid() bool {
	if strings.Contains(command.raw_text, "@tos_helper_bot") {
		return re.MatchString(command.raw_text)
	}
	return re2.MatchString(command.raw_text)
}

func (command *Command) GetErrorMessage() string {
	return ""
}

func (command *Command) ShowCardInfo(card *Card, display_mode int) string {
	var res string
	if display_mode == CARD_DISPLAY_MODE_NORMAL {
		res = fmt.Sprintf(string(miniMessageTemplate), card.Name,
			card.Rarity, card.Attribute, card.Card_id, card.Cost, card.Race, card.Series,
			card.MaxExp, card.PreviewLink, card.WikiLink, card.Name)
	} else {
		res = fmt.Sprintf(string(messageTemplate), card.Name, card.Attribute,
			card.Card_id, card.Cost, card.Race, card.Series,
			card.Rarity, card.MaxExp, card.Max_hp, card.Max_attk, card.Max_rec,
			card.TotalStats, card.ActiveSkill.Lv1cd, card.ActiveSkill.Lvmaxcd, card.ActiveSkill.Name,
			card.ActiveSkill.Effect, card.LeaderSkill.Name,
			card.LeaderSkill.Effect, card.PreviewLink, card.WikiLink, card.Name)
	}
	command.message = res
	return res

}

func (command *Command) Help() string {
	return string(helpTemplate)
}

func (command *Command) Report(s string) string {
	out, oerr := os.OpenFile("report.log", os.O_APPEND|os.O_WRONLY, 0600) //.Create("parsed.csv")
	if oerr != nil {
		return ""
	}
	defer out.Close()
	out.WriteString(s)
	out.WriteString("\n")
	return ""
}

func (command *Command) FindCardByID(card_id string, display_mode int) string {
	card := Card{}
	err := session.Model(&card).Column("ActiveSkill", "LeaderSkill").
		Where("card_id = ?", card_id).Limit(1).Select()
	if err != nil {
		fmt.Println(err)
		return command.GetErrorMessage()
	}
	return command.ShowCardInfo(&card, display_mode)

}

func (command *Command) FindCardByName(name string, display_mode int) string {
	if len(name) < 2 {
		return "Имя карты должно быть чуть длиннее 😔"
	}
	cards := make([]*Card, 3)
	err := session.Model(&cards).Column("ActiveSkill", "LeaderSkill").
		Where("card.name ilike ?", fmt.Sprintf("%%%v%%", name)).Order("card.rarity DESC").Limit(3).Select()
	if err != nil {
		log.Printf("%q", err)
	}
	fmt.Println(cards)
	if len(cards) == 0 {
		return fmt.Sprintf("Простите, мне не удалось найти такую карту 😢")
	} else if len(cards) == 1 {
		return command.ShowCardInfo(cards[0], display_mode)
	} else {
		msg := command.ShowCardInfo(cards[0], display_mode)
		markup := tgbotapi.NewInlineKeyboardMarkup()
		ids := make([]InlineQueryCard, len(cards))
		for i, v := range cards {
			ids[i] = InlineQueryCard{CardId: v.Card_id, Name: v.Name}
			if i == 0 {
				continue
			}
			row := tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(v.Name, v.Card_id),
			)
			markup.InlineKeyboard = append(markup.InlineKeyboard, row)
		}
		markup.InlineKeyboard = append(markup.InlineKeyboard, ConfirmRow)
		command.reply_markup = &markup
		queryInfo := InlineQueryInfo{
			UserId:      command.tgRequest.Message.From.ID,
			DisplayMode: display_mode,
			CardsList:   ids,
		}
		encoded, err := json.Marshal(queryInfo)
		if err != nil {
			fmt.Printf("Error while encoding: %v", err)
		}
		fmt.Printf("\n LEn encoded: ", len(encoded))
		//n := bytes.IndexByte(encoded, 0)
		nencoded := string(encoded)
		fmt.Printf("\n encoded: ")
		fmt.Println(nencoded)
		command.postData = nencoded
		return msg
	}
	return ""
}

func (command *Command) GetReplyMarkup() interface{} {
	var result interface{}
	if command.commWord == "find" || command.commWord == "f" {
		return command.reply_markup
	}
	return result
}

func (command *Command) postSave(message *tgbotapi.Message) {
	if command.commWord == "find" || command.commWord == "f" {
		client.Set(strconv.Itoa(message.MessageID), command.postData, REDIS_DEFAULT_TIMEOUT)
		fmt.Printf("Set key %v into redis", message.MessageID)
	}
}

func (command *Command) Run() string {

	if command.message != "" {
		return command.message
	}
	if !command.IsValid() {
		return command.GetErrorMessage()
	}
	var matches []string
	if strings.Contains(command.raw_text, "@tos_helper_bot") {
		matches = re.FindStringSubmatch(command.raw_text)
	} else {
		matches = re2.FindStringSubmatch(command.raw_text)
	}
	command.commWord = matches[1]
	if len(matches) == 3 {
		command.commParams = matches[2:]
	}
	switch {
	case command.commWord == "show" && command.commParams[0] != "":
		return command.FindCardByID(command.commParams[0], CARD_DISPLAY_MODE_FULL)
	case command.commWord == "s" && command.commParams[0] != "":
		return command.FindCardByID(command.commParams[0], CARD_DISPLAY_MODE_NORMAL)
	case command.commWord == "help":
		return command.Help()
	case command.commWord == "report" && command.commParams[0] != "":
		return command.Report(command.commParams[0])
	case (command.commWord == "find" || command.commWord == "f") && command.commParams[0] != "":
		return command.FindCardByName(command.commParams[0], CARD_DISPLAY_MODE_NORMAL)
	default:
		return command.GetErrorMessage()
	}
}

func applyCallbackQuery(api *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery) error {
	var rdata InlineQueryInfo
	kbd := make([][]tgbotapi.InlineKeyboardButton, 1)
	kbd[0] = make([]tgbotapi.InlineKeyboardButton, 0)
	data, err := client.Get(strconv.Itoa(query.Message.MessageID)).Result()
	if err == redis.Nil {
		return errors.New("redis key not found")
	} else if err != nil {
		return errors.New("Unknown redis error")
	}
	json.Unmarshal([]byte(data), &rdata)
	if rdata.UserId != query.From.ID {
		api.AnswerCallbackQuery(tgbotapi.NewCallback(query.ID, "У вас нет прав для этого действия"))
		return nil
	}
	if query.Data == "save" {
		fmt.Println(query.Message.Text)
		msg := tgbotapi.NewEditMessageReplyMarkup(query.Message.Chat.ID, query.Message.MessageID, tgbotapi.InlineKeyboardMarkup{kbd})
		//msg.ParseMode = "HTML"
		api.Send(msg)
		return nil
	} else if query.Data == "cancel" {
		api.Send(tgbotapi.DeleteMessageConfig{
			ChatID:    query.Message.Chat.ID,
			MessageID: query.Message.MessageID,
		})
		return nil
	}
	comm := Command{}
	cardInfo := comm.FindCardByID(query.Data, CARD_DISPLAY_MODE_NORMAL)
	if cardInfo == "" {
		return errors.New("card not found")
	}
	msg := tgbotapi.NewEditMessageText(query.Message.Chat.ID, query.Message.MessageID, cardInfo)
	markup := tgbotapi.NewInlineKeyboardMarkup()
	for _, v := range rdata.CardsList {
		if v.CardId == query.Data {
			continue
		}
		row := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(v.Name, v.CardId),
		)
		markup.InlineKeyboard = append(markup.InlineKeyboard, row)
	}
	markup.InlineKeyboard = append(markup.InlineKeyboard, ConfirmRow)
	msg.ReplyMarkup = &markup
	msg.ParseMode = "HTML"
	api.Send(msg)
	return nil
}

func main() {
	token := config.Section("telegram").Key("token").Value()
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Panic(err)
	}
	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout, _ = strconv.Atoi(config.Section("telegram").Key("timeout").Value())

	updates, err := bot.GetUpdatesChan(u)

	for update := range updates {
		log.Printf("-----------------\n")
		if update.Message != nil {

			log.Printf("[%s] %q", update.Message.From.UserName, update.Message.Text, update.Message.Chat.ID)
			command := Command{raw_text: update.Message.Text, tgRequest: &update}
			resp := command.Run()
			if resp != "" {
				fmt.Println(resp)
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, resp)
				msg.ParseMode = "HTML"
				msg.ReplyMarkup = command.GetReplyMarkup()
				message, _ := bot.Send(msg)
				command.postSave(&message)
				fmt.Println("true message id ", message.MessageID, "\n")
			}
		} else if update.CallbackQuery != nil {
			err := applyCallbackQuery(bot, update.CallbackQuery)
			if err != nil {
				fmt.Printf("Error: %v", err)
			}

		}
	}
}
