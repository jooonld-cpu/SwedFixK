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
const DefaultBankID = 1

// СТРУКТУРЫ ДАННЫХ
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
	var db *sql.DB
	var err error

	// 1. ПОДКЛЮЧЕНИЕ К БД
	dbHost := os.Getenv("DB_HOST")
	if dbHost != "" {
		dsn := fmt.Sprintf("host=%s port=5432 user=%s password='%s' dbname=%s sslmode=require",
			dbHost, os.Getenv("DB_USER"), os.Getenv("DB_PASS"), os.Getenv("DB_NAME"))
		db, err = sql.Open("postgres", dsn)
	} else {
		rawURL := os.Getenv("DATABASE_URL")
		if strings.HasPrefix(rawURL, "postgres://") || strings.HasPrefix(rawURL, "postgresql://") {
			u, parseErr := url.Parse(rawURL)
			if parseErr == nil {
				pass, _ := u.User.Password()
				dsn := fmt.Sprintf("host=%s user=%s password='%s' dbname=%s sslmode=require", 
					u.Host, u.User.Username(), pass, strings.TrimPrefix(u.Path, "/"))
				db, err = sql.Open("postgres", dsn)
			}
		}
		if db == nil {
			db, err = sql.Open("postgres", rawURL)
		}
	}

	if err != nil {
		log.Fatal("❌ Ошибка открытия БД:", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Println("❌ БД недоступна:", err)
	}

	// 2. ИНИЦИАЛИЗАЦИЯ
	db.Exec(`CREATE TABLE IF NOT EXISTS users (tg_id TEXT PRIMARY KEY, nickname TEXT, role TEXT)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS info_line (id INT PRIMARY KEY, text TEXT)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS bonds (id SERIAL PRIMARY KEY, user_id TEXT, name TEXT, amount FLOAT, rate FLOAT, created_at TIMESTAMP DEFAULT NOW(), can_withdraw BOOLEAN DEFAULT FALSE)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS available_bonds (id SERIAL PRIMARY KEY, name TEXT, price FLOAT, rate FLOAT, min_days INT DEFAULT 0)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS balances (user_id TEXT, bank_id INT, amount FLOAT)`)

	var hasBankID bool
	db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'balances' AND column_name = 'bank_id')`).Scan(&hasBankID)

	// 3. ФУНКЦИИ
	getBalance := func(userID string) (float64, error) {
		var amount float64
		var errBal error
		if hasBankID {
			errBal = db.QueryRow("SELECT COALESCE(amount, 0) FROM balances WHERE user_id = $1 AND bank_id = $2", userID, DefaultBankID).Scan(&amount)
		} else {
			errBal = db.QueryRow("SELECT COALESCE(amount, 0) FROM balances WHERE user_id = $1", userID).Scan(&amount)
		}
		return amount, errBal
	}

	setBalance := func(userID string, amount float64) error {
		var exists bool
		if hasBankID {
			db.QueryRow("SELECT EXISTS(SELECT 1 FROM balances WHERE user_id = $1 AND bank_id = $2)", userID, DefaultBankID).Scan(&exists)
			if exists {
				_, err := db.Exec("UPDATE balances SET amount = $1 WHERE user_id = $2 AND bank_id = $3", amount, userID, DefaultBankID)
				return err
			}
			_, err := db.Exec("INSERT INTO balances (user_id, bank_id, amount) VALUES ($1, $2, $3)", userID, DefaultBankID, amount)
			return err
		}
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM balances WHERE user_id = $1)", userID).Scan(&exists)
		if exists {
			_, err := db.Exec("UPDATE balances SET amount = $1 WHERE user_id = $2", amount, userID)
			return err
		}
		_, err := db.Exec("INSERT INTO balances (user_id, amount) VALUES ($1, $2)", userID, amount)
		return err
	}

	calcBond := func(amount, rate float64, createdAt time.Time) float64 {
		if createdAt.Year() < 2024 { return amount }
		days := math.Floor(time.Since(createdAt).Hours() / 24)
		if days <= 0 { return amount }
		if days > 365 { days = 365 } 
		return amount * math.Pow(1+(rate/100), days)
	}

	// 4. HTTP / API
	go func() {
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "Бот Швеции активен!")
		})
		port := os.Getenv("PORT")
		if port == "" { port = "8080" }
		http.ListenAndServe(":"+port, nil)
	}()

	// 5. BOT
	b, err := telebot.NewBot(telebot.Settings{
		Token:  os.Getenv("BOT_TOKEN"),
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil { log.Fatal(err) }

	b.Handle("/start", func(c telebot.Context) error {
		uid := strconv.FormatInt(c.Sender().ID, 10)
		var ni, ro string
		db.QueryRow("SELECT nickname, role FROM users WHERE tg_id=$1", uid).Scan(&ni, &ro)
		ba, _ := getBalance(uid)

		fURL := fmt.Sprintf("%s?tg_id=%s&exists=%t&nick=%s&role=%s&bal=%.2f",
			WebAppURL, uid, ni != "", url.QueryEscape(ni), url.QueryEscape(ro), ba)

		menu := &telebot.ReplyMarkup{ResizeKeyboard: true}
		menu.Reply(menu.Row(menu.WebApp("🇸🇪 Кабинет", &telebot.WebApp{URL: fURL})))
		return c.Send("🇸🇪 Система активна.", menu)
	})

	// АДМИН КОМАНДЫ
	b.Handle("/add_user", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		a := c.Args()
		if len(a) < 3 { return c.Send("⚠️ /add_user [ID] [NICK] [ROLE]") }
		db.Exec("INSERT INTO users (tg_id, nickname, role) VALUES ($1, $2, $3) ON CONFLICT (tg_id) DO UPDATE SET nickname=$2, role=$3", a[0], a[1], a[2])
		setBalance(a[0], 0)
		return c.Send("✅ Пользователь добавлен.")
	})

	b.Handle("/deposit", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		a := c.Args()
		if len(a) < 2 { return c.Send("ID СУММА?") }
		v, _ := strconv.ParseFloat(a[1], 64)
		cur, _ := getBalance(a[0])
		setBalance(a[0], cur+v)
		return c.Send("✅ Пополнено.")
	})

	b.Handle(telebot.OnWebApp, func(c telebot.Context) error {
		var d WebAppData
		json.Unmarshal([]byte(c.Message().WebAppData.Data), &d)
		uid := strconv.FormatInt(c.Sender().ID, 10)
		switch d.Action {
		case "buy_bond":
			var mb MarketBond
			db.QueryRow("SELECT name, price, rate FROM available_bonds WHERE id=$1", d.BondID).Scan(&mb.Name, &mb.Price, &mb.Rate)
			uB, _ := getBalance(uid)
			if uB < d.Amount { return c.Send("❌ Мало GOLD.") }
			setBalance(uid, uB-d.Amount)
			db.Exec("INSERT INTO bonds (user_id, name, amount, rate, created_at) VALUES ($1, $2, $3, $4, NOW())", uid, mb.Name, d.Amount, mb.Rate)
			return c.Send("✅ Куплено!")
		case "register":
			db.Exec("INSERT INTO users (tg_id, nickname, role) VALUES ($1, $2, $3)", uid, d.Nick, d.Role)
			setBalance(uid, 0)
			return c.Send("✅ Регистрация.")
		}
		return nil
	})

	log.Println("🚀 Бот запущен!")
	b.Start()
}
