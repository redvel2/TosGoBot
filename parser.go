package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"github.com/satori/go.uuid"
)

const SKILL_TYPE_ACTIVE = 1
const SKILL_TYPE_LEADER = 2

var CARD_ATTRIBUTE_REGEX = regexp.MustCompile(`<[^>]+>`)

type Skill struct {
	Id      string
	Name    string
	Lv1CD   int
	LvMaxCD int
	Effect  string
	Type    int
}

type SafeMap struct {
	mx    sync.Mutex
	value map[string]*Skill
}

var skillMap = SafeMap{value: make(map[string]*Skill)}

func (skill *Skill) GetRow() []string {
	return []string{skill.Id, skill.Name, strconv.Itoa(skill.Lv1CD), strconv.Itoa(skill.LvMaxCD), skill.Effect, strconv.Itoa(skill.Type)}
}

func NewSkill(skill_type int) Skill {
	skill := Skill{Type: skill_type}
	uid, _ := uuid.NewV4()
	skill.Id = uid.String()
	return skill
}

type Card struct {
	Id          string
	Name        string
	Attribute   string
	Rarity      int
	Cost        int
	Race        string
	Series      string
	MaxExp      int
	M_Hp        int
	M_Att       int
	M_Rec       int
	TotalStats  int
	ActiveSkill *Skill
	LeaderSkill *Skill
	WikiLink    string
	PreviewLink string
}

func NewCard() Card {
	card := Card{}
	//card.ActiveSkill = SKILL_TYPE_ACTIVE
	//card.LeaderSkill.Type = SKILL_TYPE_LEADER
	return card
}

func (card *Card) Parse(doc *goquery.Document) error {
	doc.Find("article table.shadow td").Each(func(i int, s *goquery.Selection) {
		//fmt.Println(s.Text(), "\nlala")
		switch i {
		case 0:
			img_url, _ := s.Find("img").Attr("data-src")
			card.PreviewLink = img_url
		case 1:
			card.Name = strings.Replace(s.Text(), "\n", "", -1)
		case 2:
			card.Attribute = ReplaceWSpace(CARD_ATTRIBUTE_REGEX.ReplaceAllString(s.Text(), ""))
		case 3:
			card.Id = ReplaceWSpace(strings.Replace(s.Text(), "No.", "", -1))
		case 4:
			rarity, _ := strconv.Atoi(ReplaceWSpace(strings.Replace(s.Text(), "★", "", -1)))
			card.Rarity = rarity
		case 5:
			cost, _ := strconv.Atoi(ReplaceWSpace(s.Text()))
			card.Cost = cost
		case 6:
			card.Race = ReplaceWSpace(CARD_ATTRIBUTE_REGEX.ReplaceAllString(s.Text(), ""))
		case 7:
			card.Series = ReplaceWSpace(CARD_ATTRIBUTE_REGEX.ReplaceAllString(s.Text(), ""))
		case 10:
			max_exp, _ := strconv.Atoi(strings.Replace(ReplaceWSpace(s.Text()), ",", "", -1))
			card.MaxExp = max_exp
		case 18:
			card.M_Hp = SumStats(ReplaceWSpace(s.Text()))
		case 19:
			card.M_Att = SumStats(ReplaceWSpace(s.Text()))
		case 20:
			card.M_Rec = SumStats(ReplaceWSpace(s.Text()))
		case 21:
			card.TotalStats = SumStats(ReplaceWSpace(s.Text()))
		case 25:
			skill_name := ReplaceRN(CARD_ATTRIBUTE_REGEX.ReplaceAllString(s.Text(), ""))
			skillMap.mx.Lock()
			skill, exists := skillMap.value[skill_name]
			if !exists {
				skill := NewSkill(SKILL_TYPE_ACTIVE)
				skill.Name = skill_name
				card.ActiveSkill = &skill
				skillMap.value[skill_name] = card.ActiveSkill
			} else {
				card.ActiveSkill = skill
			}
			skillMap.mx.Unlock()
		case 26:
			skill_cd, _ := strconv.Atoi(ReplaceWSpace(s.Text()))
			card.ActiveSkill.Lv1CD = skill_cd
		case 27:
			skill_cd, _ := strconv.Atoi(ReplaceWSpace(s.Text()))
			card.ActiveSkill.LvMaxCD = skill_cd
		case 28:
			card.ActiveSkill.Effect = ReplaceRN(CARD_ATTRIBUTE_REGEX.ReplaceAllString(s.Text(), ""))
		case 30:
			skill_name := ReplaceRN(CARD_ATTRIBUTE_REGEX.ReplaceAllString(s.Text(), ""))
			skillMap.mx.Lock()
			skill, exists := skillMap.value[skill_name]
			if !exists {
				skill := NewSkill(SKILL_TYPE_LEADER)
				skill.Name = skill_name
				card.LeaderSkill = &skill
				skillMap.value[skill_name] = card.LeaderSkill
			} else {
				card.LeaderSkill = skill
			}
			skillMap.mx.Unlock()
		case 31:
			card.LeaderSkill.Effect = ReplaceRN(CARD_ATTRIBUTE_REGEX.ReplaceAllString(s.Text(), ""))
		}

	})
	//card.SavePreview()
	return nil
}

func (card *Card) SavePreview() bool {
	if card.PreviewLink == "" {
		return false
	}
	save_path := fmt.Sprintf("C:/Users/unitt/preview/%v.jpg", card.Id)
	out, err := os.Create(save_path)
	if err != nil {
		log.Printf("[Error] Eror while saving picture for card with id %v. Original error was %v\n", card.Id, err)
		return false
	}
	defer out.Close()
	resp, rerr := http.Get(card.PreviewLink)
	if rerr != nil {
		log.Printf("[Error] Eror while saving picture for card with id %v. Can`t get contents. Original error was %v\n", card.Id, rerr)
		return false
	}
	defer resp.Body.Close()
	io.Copy(out, resp.Body)
	return true
}

func GetCardHeaders() []string {
	arr := []string{"card_id", "name", "attribute", "rariry", "cost", "race", "series", "max_exp", "max_hp", "max_attk", "max_rec", "total_stats", "wiki_link", "preview_link"}
	return arr
}

func (card *Card) GetRow() []string {
	arr := []string{card.Id, card.Name, card.Attribute, strconv.Itoa(card.Rarity), strconv.Itoa(card.Cost), card.Race, card.Series, strconv.Itoa(card.MaxExp), strconv.Itoa(card.M_Hp), strconv.Itoa(card.M_Att), strconv.Itoa(card.M_Rec), strconv.Itoa(card.TotalStats), card.WikiLink, card.PreviewLink, card.ActiveSkill.Id, card.LeaderSkill.Id}
	return arr
}

func ReplaceWSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

func ReplaceRN(s string) string {
	return strings.Replace(strings.Replace(s, "\n", "", -1), "\r", "", -1)
}

func SumStats(x string) int {
	sum := 0
	if strings.Contains(x, "+") {
		for _, val := range strings.Split(x, "+") {
			v, _ := strconv.Atoi(val)
			sum += v
		}
	} else {
		v, _ := strconv.Atoi(x)
		sum = v
	}
	return sum
}

func _check(err error) {
	if err != nil {
		panic(err)
	}
}

// основная функция обработки
func parseUrl(url string) []string {
	result := make([]string, 0, 50)
	card := NewCard()
	card.WikiLink = url
	// заворачиваем источник в goquery документ
	doc, err := goquery.NewDocument(url)
	_check(err)
	// в манере jquery, css селектором получаем все ссылки
	doc.Find("table.shadow td[style='font-size: 1.2em'] b a").Each(func(i int, s *goquery.Selection) {
		attr, hasattr := s.Attr("href")
		if !hasattr {
			fmt.Println("No attr hre found")
		} else {
			result = append(result, fmt.Sprintf("http://towerofsaviors.wikia.com%v", attr))
		}
	})
	fmt.Println(result)
	return result
}

func main() {
	// doc, _ := goquery.NewDocument("http://towerofsaviors.wikia.com/wiki/Poker_King_-_Paxton")
	// card := NewCard()
	// card.Parse(doc)
	// fmt.Printf("%+v", card)
	var wg sync.WaitGroup
	var counter int
	f, err := os.OpenFile("tos_cards_parser.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		panic("Eror while creating logfile")
	}
	defer f.Close()
	log.SetOutput(f)

	out, oerr := os.Create("parsed.csv")
	if oerr != nil {
		fmt.Println(oerr)
	}
	defer out.Close()
	w := csv.NewWriter(out)
	w.Comma = '$'

	out2, oerr2 := os.Create("parsed_skills.csv")
	if oerr2 != nil {
		fmt.Println(oerr2)
	}
	defer out.Close()
	w2 := csv.NewWriter(out2)
	w2.Comma = '$'

	//w.Write(GetCardHeaders())

	// получаем список url из входных параметров
	patterns := [5]string{
		"http://towerofsaviors.wikia.com/wiki/Gallery_P%02d-P%02d",
		"http://towerofsaviors.wikia.com/wiki/Gallery_S%02d-S%02d",
		"http://towerofsaviors.wikia.com/wiki/Gallery_V%02d-V%02d",
		"http://towerofsaviors.wikia.com/wiki/Gallery_M%02d-M%02d",
		"http://towerofsaviors.wikia.com/wiki/Gallery_%03d-%03d",
	}
	iterations := [5]int{1, 4, 1, 1, 36}
	log.Printf("--------Cards parse process started-------\n")
	for idx, pattern := range patterns {
		for i := 0; i < iterations[idx]; i++ {
			arr := make([]*Card, 50)
			// каждый выполним параллельно
			url := fmt.Sprintf(pattern, 50*i+1, 50*(i+1))
			fmt.Println("Processing : ", url)
			for k, val := range parseUrl(url) {
				wg.Add(1)
				go func(i int, url string) {
					fmt.Println("Evaluating ", url, " ", i)
					defer wg.Done()
					card := NewCard()
					card.WikiLink = url
					doc, err := goquery.NewDocument(url)
					_check(err)
					card.Parse(doc)
					arr[i] = &card
				}(k, val)
			}
			wg.Wait()
			for _, v := range arr {
				if v == nil {
					continue
				}
				w.Write(v.GetRow())
				counter++
			}
			time.Sleep(10 * time.Second)
			// закрываем в анонимной функции переменную из цикла,
			// что бы предотвартить её потерю во время обработки
		}
	}

	w.Flush()

	for _, v := range skillMap.value {
		w2.Write(v.GetRow())
	}
	w2.Flush()
	log.Printf("--------Cards parse FINISHED. Total %v rows written\n", counter)

	// ждем завершения процессов
}
