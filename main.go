package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"gopkg.in/telebot.v3"
)

const AdminID = 7631664265
const WebAppURL = "https://jooonld-cpu.github.io/SwedenFixKFront.github.io/"

type MarketBond struct {
	ID      int     `json:"id"`
	Name    string  `json:"name"`
	Price   float64 `json:"price"`
	Rate    float64 `json:"rate"`
	MinDays int     `json:"min_days"`
}

type Bond struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	Amount       float64 `json:"amount"`
	Rate         float64 `json:"rate"`
	CurrentValue float64 `json:"current_value"`
	Date         string  `json:"date"`
	CanWithdraw  bool    `json:"can_withdraw"`
}

type UserShort struct {
	ID   string `json:"id"`
	Nick string `json:"nick"`
}

type WebAppData struct {
	Action   string  `json:"action"`
	Nick     string  `json:"nick"`
	Role     string  `json:"role"`
	TargetID string  `json:"target_id"`
	Amount   float64 `json:"amount"`
	BondID   int     `json:"bond_id"`
}

func main() {
	dsn := os.Getenv("DATABASE_URL")
	db, err := sql.Open("postgres", dsn)
	if err != nil { log.Fatal(err) }
	defer db.Close()

	// Инициализация таблиц
	db.Exec(`CREATE TABLE IF NOT EXISTS users (tg_id TEXT PRIMARY KEY, nickname TEXT, role TEXT)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS info_line (id INT PRIMARY KEY, text TEXT)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS bonds (id SERIAL PRIMARY KEY, user_id TEXT, name TEXT, amount FLOAT, rate FLOAT, created_at TIMESTAMP DEFAULT NOW(), can_withdraw BOOLEAN DEFAULT FALSE)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS available_bonds (id SERIAL PRIMARY KEY, name TEXT, price FLOAT, rate FLOAT, min_days INT DEFAULT 0)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS balances (user_id TEXT PRIMARY KEY, amount FLOAT DEFAULT 0)`)

	getBalance := func(uid string) float64 {
		var a float64
		db.QueryRow("SELECT COALESCE(amount, 0) FROM balances WHERE user_id=$1", uid).Scan(&a)
		return a
	}

	setBalance := func(uid string, a float64) {
		db.Exec("INSERT INTO balances (user_id, amount) VALUES ($1, $2) ON CONFLICT (user_id) DO UPDATE SET amount=$2", uid, a)
	}

	calcBond := func(amount, rate float64, t time.Time) float64 {
		days := math.Floor(time.Since(t).Hours() / 24)
		if days <= 0 { return amount }
		return amount * math.Pow(1+(rate/100), days)
	}

	// API для твоего HTML
	go func() {
		http.HandleFunc("/api/get_user_data", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			uid := r.URL.Query().Get("uid")
			var info string
			db.QueryRow("SELECT text FROM info_line WHERE id=1").Scan(&info)
			
			rows, _ := db.Query("SELECT id, name, amount, rate, created_at, can_withdraw FROM bonds WHERE user_id=$1", uid)
			var userBonds []Bond
			for rows != nil && rows.Next() {
				var b Bond; var ct time.Time
				rows.Scan(&b.ID, &b.Name, &b.Amount, &b.Rate, &ct, &b.CanWithdraw)
				b.CurrentValue = calcBond(b.Amount, b.Rate, ct)
				b.Date = ct.Format("02.01.2006")
				userBonds = append(userBonds, b)
			}
			if rows != nil { rows.Close() }

			json.NewEncoder(w).Encode(map[string]interface{}{
				"balance": getBalance(uid),
				"info":    info,
				"bonds":   userBonds,
			})
		})
		http.ListenAndServe(":"+os.Getenv("PORT"), nil)
	}()

	b, _ := telebot.NewBot(telebot.Settings{
		Token:  os.Getenv("BOT_TOKEN"),
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	})

	// АДМИН КОМАНДЫ
	b.Handle("/all_bonds", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		rows, _ := db.Query("SELECT b.id, u.nickname, b.name, b.amount FROM bonds b JOIN users u ON b.user_id = u.tg_id")
		defer rows.Close()
		res := "📈 Все вклады:\n"
		for rows.Next() {
			var id int; var nick, name string; var am float64
			rows.Scan(&id, &nick, &name, &am)
			res += fmt.Sprintf("[%d] %s: %s (%.2f)\n", id, nick, name, am)
		}
		return c.Send(res)
	})

	b.Handle("/cash_all_file", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		rows, _ := db.Query("SELECT u.nickname, b.amount FROM balances b JOIN users u ON b.user_id = u.tg_id")
		defer rows.Close()
		content := "РЕЕСТР БАЛАНСОВ\n"
		for rows.Next() {
			var n string; var a float64
			rows.Scan(&n, &a)
			content += fmt.Sprintf("%s: %.2f GOLD\n", n, a)
		}
		os.WriteFile("balances.txt", []byte(content), 0644)
		return c.Send(&telebot.Document{File: telebot.FromDisk("balances.txt"), FileName: "balances.txt"})
	})

	b.Handle("/deposit", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		args := c.Args()
		if len(args) < 2 { return c.Send("ID Сумма?") }
		v, _ := strconv.ParseFloat(args[1], 64)
		setBalance(args[0], getBalance(args[0])+v)
		return c.Send("✅ Пополнено")
	})

	b.Handle("/start", func(c telebot.Context) error {
		uid := strconv.FormatInt(c.Sender().ID, 10)
		var ni, ro string
		db.QueryRow("SELECT nickname, role FROM users WHERE tg_id=$1", uid).Scan(&ni, &ro)
		
		// Собираем данные для WebApp
		rowsU, _ := db.Query("SELECT tg_id, nickname FROM users")
		var uL []UserShort
		for rowsU.Next() {
			var u UserShort; rowsU.Scan(&u.ID, &u.Nick); uL = append(uL, u)
		}
		uJ, _ := json.Marshal(uL)

		rowsM, _ := db.Query("SELECT id, name, price, rate FROM available_bonds")
		var mL []MarketBond
		for rowsM.Next() {
			var m MarketBond; rowsM.Scan(&m.ID, &m.Name, &m.Price, &m.Rate); mL = append(mL, m)
		}
		mJ, _ := json.Marshal(mL)

		fURL := fmt.Sprintf("%s?tg_id=%s&exists=%t&nick=%s&role=%s&bal=%.2f&users=%s&market=%s",
			WebAppURL, uid, ni != "", url.QueryEscape(ni), url.QueryEscape(ro), getBalance(uid), 
			url.QueryEscape(string(uJ)), url.QueryEscape(string(mJ)))

		menu := &telebot.ReplyMarkup{ResizeKeyboard: true}
		menu.Reply(menu.Row(menu.WebApp("🇸🇪 Кабинет", &telebot.WebApp{URL: fURL})))
		return c.Send("🇸🇪 Система активна.", menu)
	})

	b.Handle(telebot.OnWebApp, func(c telebot.Context) error {
		var d WebAppData
		json.Unmarshal([]byte(c.Message().WebAppData.Data), &d)
		uid := strconv.FormatInt(c.Sender().ID, 10)

		switch d.Action {
		case "register":
			db.Exec("INSERT INTO users (tg_id, nickname, role) VALUES ($1, $2, $3) ON CONFLICT (tg_id) DO UPDATE SET nickname=$2, role=$3", uid, d.Nick, d.Role)
			return c.Send("✅ Регистрация завершена!")
		case "transfer":
			cur := getBalance(uid)
			if cur < d.Amount { return c.Send("❌ Мало GOLD") }
			setBalance(uid, cur-d.Amount)
			setBalance(d.TargetID, getBalance(d.TargetID)+d.Amount)
			return c.Send("✅ Перевод выполнен")
		case "withdraw":
			b.Send(&telebot.User{ID: AdminID}, fmt.Sprintf("⚠️ ЗАПРОС НА ВЫВОД: %s | %.2f GOLD", d.Nick, d.Amount))
			return c.Send("✅ Запрос отправлен")
		}
		return nil
	})

	log.Println("🚀 Бот запущен!")
	b.Start()
}
