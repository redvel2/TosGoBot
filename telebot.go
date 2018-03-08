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
	"time"

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

var pollChan = make(chan *Poll)

var client = redis.NewClient(&redis.Options{
	Addr:     "localhost:6379",
	Password: "", // no password set
	DB:       0,  // use default DB
})

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

const REDIS_DEFAULT_TIMEOUT = 0
const CARD_DISPLAY_MODE_NORMAL = 1
const CARD_DISPLAY_MODE_FULL = 2
const POLL_DEFAULT_DURATION = time.Second * 60 * 60 * 24

type Vote struct {
	Id     int
	PollId int
	Poll   *Poll
	Name   string
	Count  int
}

type Poll struct {
	Id          int
	Name        string
	Created     time.Time
	ActiveUntil time.Time
	UserId      int
	MessageId   int
	ChatId      int64
}

type PollUser struct {
	PollId int
	Poll   *Poll
	UserId int
}

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
	message      *tgbotapi.MessageConfig
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

func (command *Command) GetErrorMessage() error {
	return errors.New("Error while running command")
}

func (command *Command) NewMessage(text string) *tgbotapi.MessageConfig {
	var chatId int64
	if command.tgRequest.CallbackQuery != nil {
		chatId = command.tgRequest.CallbackQuery.Message.Chat.ID
	} else {
		chatId = command.tgRequest.Message.Chat.ID
	}
	msg := tgbotapi.NewMessage(chatId, text)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = command.GetReplyMarkup()
	command.message = &msg
	return &msg
}

func (command *Command) EmptyMessage() *tgbotapi.MessageConfig {
	return command.NewMessage("")
}

func (command *Command) ShowCardInfo(card *Card, display_mode int) *tgbotapi.MessageConfig {
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
	fmt.Printf("Res is %q", res)
	return command.NewMessage(res)

}

func (command *Command) Help(api *tgbotapi.BotAPI) error {
	msg := command.NewMessage(string(helpTemplate))
	api.Send(msg)
	return nil
}

func (command *Command) Report(api *tgbotapi.BotAPI, s string) error {
	out, oerr := os.OpenFile("report.log", os.O_APPEND|os.O_WRONLY, 0600) //.Create("parsed.csv")
	if oerr != nil {
		return errors.New("Error while opening report file")
	}
	defer out.Close()
	out.WriteString(s)
	out.WriteString("\n")
	return nil
}

func (command *Command) GetCardById(cardId string) (*Card, error) {
	card := Card{}
	err := session.Model(&card).Column("ActiveSkill", "LeaderSkill").
		Where("card_id = ?", cardId).Limit(1).Select()
	return &card, err
}

func (command *Command) FindCardByID(api *tgbotapi.BotAPI, cardId string, display_mode int) error {
	card, err := command.GetCardById(cardId)
	if err != nil {
		return err
	}
	msg := command.ShowCardInfo(card, display_mode)
	api.Send(msg)
	return nil

}

func (command *Command) FindCardByName(api *tgbotapi.BotAPI, name string, display_mode int) error {

	var ConfirmRow = tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("✅ Ок", command.NewQuery("save")),
		tgbotapi.NewInlineKeyboardButtonData("🚫 Отменить", command.NewQuery("cancel")),
	)

	if len(name) < 2 {
		msg := command.NewMessage("Имя карты должно быть чуть длиннее 😔")
		api.Send(msg)
		return nil
	}
	cards := make([]*Card, 3)
	err := session.Model(&cards).Column("ActiveSkill", "LeaderSkill").
		Where("card.name ilike ?", fmt.Sprintf("%%%v%%", name)).Order("card.rarity DESC").Limit(3).Select()
	if err != nil {
		log.Printf("%q", err)
	}
	fmt.Println(cards)
	if len(cards) == 0 {
		msg := command.NewMessage(fmt.Sprintf("Простите, мне не удалось найти такую карту 😢"))
		api.Send(msg)
		return nil
	} else if len(cards) == 1 {
		msg := command.ShowCardInfo(cards[0], display_mode)
		api.Send(msg)
		return nil
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
				tgbotapi.NewInlineKeyboardButtonData(v.Name, command.NewQuery(v.Card_id)),
			)
			markup.InlineKeyboard = append(markup.InlineKeyboard, row)
		}
		markup.InlineKeyboard = append(markup.InlineKeyboard, ConfirmRow)
		msg.ReplyMarkup = &markup
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
		message, merr := api.Send(msg)
		if merr != nil {
			return merr
		}
		client.Set(strconv.Itoa(message.MessageID), nencoded, REDIS_DEFAULT_TIMEOUT)
		return nil
	}
	return command.GetErrorMessage()
}

func (command *Command) GetReplyMarkup() interface{} {
	var result interface{}
	if command.commWord == "find" || command.commWord == "f" {
		return command.reply_markup
	}
	return result
}

func (command *Command) NewQuery(data ...string) string {
	pattern := strings.Repeat("%v:", len(data)+1)
	new := make([]interface{}, len(data)+1)
	new[0] = command.commWord
	for i := 1; i < len(data)+1; i++ {
		new[i] = data[i-1]
	}
	return fmt.Sprintf(pattern[:len(pattern)-1], new...)
}

func (command *Command) parseQuery(data string) ([]string, error) {
	var emptyResult []string
	result := strings.Split(data, ":")
	if len(result) < 2 {
		return emptyResult, errors.New("Wrong inline query")
	}
	return result, nil
}

func (command *Command) postSave(message *tgbotapi.Message) {
	if command.commWord == "find" || command.commWord == "f" {
		client.Set(strconv.Itoa(message.MessageID), command.postData, REDIS_DEFAULT_TIMEOUT)
		fmt.Printf("Set key %v into redis", message.MessageID)
	}
}

func (command *Command) NewPoll(api *tgbotapi.BotAPI) error {
	if command.tgRequest.Message.ReplyToMessage == nil {
		api.Send(command.NewMessage("Вы не указали сообщение для голосования 😔"))
		return nil
	}

	poll := Poll{
		Name:        "Poll 1",
		Created:     time.Now(),
		ActiveUntil: time.Now().Add(POLL_DEFAULT_DURATION),
		UserId:      command.tgRequest.Message.From.ID,
		ChatId:      command.tgRequest.Message.Chat.ID,
	}
	err := session.Insert(&poll)
	if err != nil {
		return err
	}
	vote1 := Vote{Name: "👍 За", PollId: poll.Id}
	vote2 := Vote{Name: "👎 Против", PollId: poll.Id}
	_, verr := session.Model(&vote1, &vote2).Insert()
	if verr != nil {
		return verr
	}

	msg := command.NewMessage("Выберите 1 из вариантов:")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%v (0)", vote1.Name),
			command.NewQuery(strconv.Itoa(poll.Id), strconv.Itoa(vote1.Id))),
		tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%v (0)", vote2.Name),
			command.NewQuery(strconv.Itoa(poll.Id), strconv.Itoa(vote2.Id)),
		)))
	msg.ReplyToMessageID = command.tgRequest.Message.ReplyToMessage.MessageID
	message, _ := api.Send(msg)
	poll.MessageId = message.MessageID
	_, qerr := session.Model(&poll).Set("message_id = ?message_id").Update()
	if qerr == nil {
		pollChan <- &poll
	}
	return qerr
}

func (command *Command) Run(api *tgbotapi.BotAPI) error {

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
		return command.FindCardByID(api, command.commParams[0], CARD_DISPLAY_MODE_FULL)
	case command.commWord == "s" && command.commParams[0] != "":
		return command.FindCardByID(api, command.commParams[0], CARD_DISPLAY_MODE_NORMAL)
	case command.commWord == "help":
		return command.Help(api)
	case command.commWord == "report" && command.commParams[0] != "":
		return command.Report(api, command.commParams[0])
	case (command.commWord == "find" || command.commWord == "f") && command.commParams[0] != "":
		return command.FindCardByName(api, command.commParams[0], CARD_DISPLAY_MODE_NORMAL)
	case command.commWord == "poll":
		return command.NewPoll(api)
	default:
		return command.GetErrorMessage()
	}
}

func (command *Command) applyCallbackQuery(api *tgbotapi.BotAPI) error {
	var rdata InlineQueryInfo
	comm := Command{}
	query := command.tgRequest.CallbackQuery
	kbd := make([][]tgbotapi.InlineKeyboardButton, 1)
	kbd[0] = make([]tgbotapi.InlineKeyboardButton, 0)

	dArr, dErr := comm.parseQuery(query.Data)
	if dErr != nil {
		return dErr
	}

	commandWord, queryData := dArr[0], dArr[1]
	command.commWord = commandWord
	switch {
	case queryData == "save" && (commandWord == "f" || commandWord == "find"):

		fmt.Println(query.Message.Text)
		msg := tgbotapi.NewEditMessageReplyMarkup(query.Message.Chat.ID, query.Message.MessageID, tgbotapi.InlineKeyboardMarkup{kbd})
		//msg.ParseMode = "HTML"
		api.Send(msg)
		return nil
	case queryData == "cancel" && (commandWord == "f" || commandWord == "find"):
		api.Send(tgbotapi.DeleteMessageConfig{
			ChatID:    query.Message.Chat.ID,
			MessageID: query.Message.MessageID,
		})
		return nil
	case commandWord == "f" || commandWord == "find":
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
		msg, err := command.queryCardId(
			queryData,
			query,
			&rdata,
		)
		if err != nil {
			return err
		}
		api.Send(msg)
		return nil
	case commandWord == "poll" && len(dArr) == 3:
		voteId, _ := strconv.Atoi(dArr[2])
		pollId, _ := strconv.Atoi(dArr[1])

		count, _ := session.Model(&PollUser{}).Where("user_id = ? and poll_id = ?", query.From.ID, pollId).Count()
		if count > 0 {
			api.AnswerCallbackQuery(tgbotapi.NewCallback(query.ID, "Вы уже голосовали в этом опросе"))
			return nil
		}

		poll := Poll{Id: pollId}
		vote := Vote{Id: voteId}
		err := session.Select(&vote)
		if err != nil {
			return err
		}
		_, ierr := session.Model(&PollUser{PollId: pollId, UserId: query.From.ID}).Insert()
		if ierr != nil {
			api.AnswerCallbackQuery(tgbotapi.NewCallback(query.ID, "Ошибка. Сервис недоступен"))
			return ierr
		}
		session.Model(&vote).Set("count = count + 1").Update()
		session.Model(&poll).Set("modified = true").Update()
		api.AnswerCallbackQuery(tgbotapi.NewCallback(query.ID, "Спасибо, ваш голос учтен"))
		return nil
	}
	return nil
}

func (command *Command) queryCardId(cardId string, query *tgbotapi.CallbackQuery, rdata *InlineQueryInfo) (*tgbotapi.EditMessageTextConfig, error) {
	var ConfirmRow = tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("✅ Ок", command.NewQuery("save")),
		tgbotapi.NewInlineKeyboardButtonData("🚫 Отменить", command.NewQuery("cancel")),
	)

	var EmptyResult tgbotapi.EditMessageTextConfig
	fmt.Printf("Card id is %q \n", cardId)
	card, err := command.GetCardById(cardId)
	if err != nil {
		return &EmptyResult, err
	}
	cardInfo := command.ShowCardInfo(card, CARD_DISPLAY_MODE_NORMAL)
	msg := tgbotapi.NewEditMessageText(query.Message.Chat.ID, query.Message.MessageID, cardInfo.Text)
	markup := tgbotapi.NewInlineKeyboardMarkup()
	for _, v := range rdata.CardsList {
		if v.CardId == cardId {
			continue
		}
		row := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(v.Name, command.NewQuery(v.CardId)),
		)
		markup.InlineKeyboard = append(markup.InlineKeyboard, row)
	}
	markup.InlineKeyboard = append(markup.InlineKeyboard, ConfirmRow)
	msg.ReplyMarkup = &markup
	msg.ParseMode = "HTML"
	return &msg, nil
}

func watchActivePolls(api *tgbotapi.BotAPI, c chan *Poll) {
	var polls []*Poll
	var votes []Vote
	command := Command{commWord: "poll"}
	err := session.Model(&polls).Where("active_until > ?", time.Now().Format(time.RFC3339)).Select()
	if err != nil {
		fmt.Printf("%v", err)
	}
	for {
		actualPolls := make([]*Poll, 0)
		select {
		case newPoll := <-c:
			polls = append(polls, newPoll)
		default:
			for _, poll := range polls {
				if poll.ActiveUntil.Before(time.Now()) {
					continue
				}
				actualPolls = append(actualPolls, poll)
				err := session.Model(&votes).Column("Poll").Where("poll_id = ? and poll.modified=true", poll.Id).Order("id").Select()
				if err != nil {
					fmt.Println(err)
					continue
				}
				if len(votes) == 0 {
					continue
				}
				buttons := make([]tgbotapi.InlineKeyboardButton, len(votes))
				for i, vote := range votes {
					buttons[i] = tgbotapi.NewInlineKeyboardButtonData(
						fmt.Sprintf("%v (%v)", vote.Name, vote.Count),
						command.NewQuery(strconv.Itoa(poll.Id), strconv.Itoa(vote.Id)),
					)
				}
				msg := tgbotapi.NewEditMessageReplyMarkup(poll.ChatId, poll.MessageId, tgbotapi.NewInlineKeyboardMarkup(buttons))
				api.Send(msg)
				_, uerr := session.Model(poll).Set("modified = false").Update()
				if uerr != nil {
					fmt.Println(uerr)
				}
			}
			polls = actualPolls
			time.Sleep(2 * time.Second)
		}
	}
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
	go watchActivePolls(bot, pollChan)
	for update := range updates {
		log.Printf("-----------------\n")
		if update.Message != nil {

			log.Printf("[%s] %q", update.Message.From.UserName, update.Message.Text, update.Message.Chat.ID)
			command := Command{raw_text: update.Message.Text, tgRequest: &update}
			err := command.Run(bot)
			if err != nil {
				fmt.Println(err)
			}
		} else if update.CallbackQuery != nil {
			command := Command{tgRequest: &update}
			err := command.applyCallbackQuery(bot)
			if err != nil {
				fmt.Printf("Error: %v", err)
			}

		}
	}
}
