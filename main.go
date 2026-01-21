package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http" // Добавлено
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
	// --- ДОБАВЛЕННЫЙ HTTP БЛОК ДЛЯ RENDER ---
	go func() {
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "Бот Швеции активен!")
		})
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
		log.Println("🌍 HTTP Сервер запущен на порту " + port)
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			log.Fatal(err)
		}
	}()
	// ----------------------------------------

	db, err := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	b, err := telebot.NewBot(telebot.Settings{
		Token:  os.Getenv("BOT_TOKEN"),
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		log.Fatal(err)
	}

	// ИНИЦИАЛИЗАЦИЯ
	db.Exec(`CREATE TABLE IF NOT EXISTS users (tg_id TEXT PRIMARY KEY, nickname TEXT, role TEXT)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS info_line (id INT PRIMARY KEY, text TEXT)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS bonds (id SERIAL PRIMARY KEY, user_id TEXT, name TEXT, amount FLOAT, rate FLOAT, created_at TIMESTAMP DEFAULT NOW(), can_withdraw BOOLEAN DEFAULT FALSE)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS available_bonds (id SERIAL PRIMARY KEY, name TEXT, price FLOAT, rate FLOAT, min_days INT DEFAULT 0)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS balances (user_id TEXT, bank_id INT, amount FLOAT)`)
	
	db.Exec(`ALTER TABLE bonds ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT NOW()`)
	db.Exec(`ALTER TABLE bonds ADD COLUMN IF NOT EXISTS can_withdraw BOOLEAN DEFAULT FALSE`)
	db.Exec(`ALTER TABLE available_bonds ADD COLUMN IF NOT EXISTS min_days INT DEFAULT 0`)

	var hasBankID bool
	db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'balances' AND column_name = 'bank_id')`).Scan(&hasBankID)

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
		res := amount * math.Pow(1+(rate/100), days)
		if math.IsInf(res, 0) || math.IsNaN(res) { return amount }
		return res
	}

	// CALLBACK ОБРАБОТЧИКИ
	b.Handle(telebot.OnCallback, func(c telebot.Context) error {
		data := c.Callback().Data
		if strings.Contains(data, "approve") {
			parts := strings.Split(data, ":")
			if len(parts) < 3 { return c.Respond() }
			targetID := parts[1]
			amount, _ := strconv.ParseFloat(parts[2], 64)
			cur, _ := getBalance(targetID)
			setBalance(targetID, cur-amount)
			tID, _ := strconv.ParseInt(targetID, 10, 64)
			b.Send(&telebot.User{ID: tID}, fmt.Sprintf("✅ Вывод одобрен: %.2f GOLD.", amount))
			c.Edit(fmt.Sprintf("✅ ОДОБРЕНО\n💰 Сумма: %.2f", amount))
			return c.Respond()
		}
		if strings.Contains(data, "reject") {
			parts := strings.Split(data, ":")
			tID, _ := strconv.ParseInt(parts[1], 10, 64)
			b.Send(&telebot.User{ID: tID}, "❌ Вывод отклонен.")
			c.Edit("❌ ОТКЛОНЕНО")
			return c.Respond()
		}
		return c.Respond()
	})

	// --- АДМИН КОМАНДЫ ---

	b.Handle("/cash_all", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		rows, _ := db.Query("SELECT u.nickname, b.amount FROM balances b JOIN users u ON b.user_id = u.tg_id")
		defer rows.Close()
		var res strings.Builder
		res.WriteString("💰 **БАЛАНСЫ:**\n\n")
		for rows.Next() {
			var n string; var a float64
			rows.Scan(&n, &a); res.WriteString(fmt.Sprintf("%s: %.2f GOLD\n", n, a))
		}
		return c.Send(res.String(), telebot.ModeMarkdown)
	})

	b.Handle("/cash_all_file", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		rows, _ := db.Query("SELECT user_id, amount FROM balances")
		defer rows.Close()
		txt := "ID | BALANCE\n"
		for rows.Next() {
			var id string; var a float64
			rows.Scan(&id, &a); txt += fmt.Sprintf("%s | %.2f\n", id, a)
		}
		os.WriteFile("balances.txt", []byte(txt), 0644)
		defer os.Remove("balances.txt")
		return c.Send(&telebot.Document{File: telebot.FromDisk("balances.txt")})
	})

	b.Handle("/add_user", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		a := c.Args()
		if len(a) < 3 { return c.Send("⚠️ /add_user [ID] [NICK] [ROLE]") }
		db.Exec("INSERT INTO users (tg_id, nickname, role) VALUES ($1, $2, $3) ON CONFLICT (tg_id) DO UPDATE SET nickname=$2, role=$3", a[0], a[1], a[2])
		setBalance(a[0], 0)
		return c.Send("✅ Готово.")
	})

	b.Handle("/del_user", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		if len(c.Args()) < 1 { return c.Send("ID?") }
		db.Exec("DELETE FROM users WHERE tg_id=$1", c.Args()[0])
		return c.Send("🗑 Удален.")
	})

	b.Handle("/deposit", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		a := c.Args()
		if len(a) < 2 { return c.Send("ID SUMMA?") }
		v, _ := strconv.ParseFloat(a[1], 64)
		cur, _ := getBalance(a[0])
		setBalance(a[0], cur+v)
		return c.Send("✅")
	})

	// --- МЕНЮ START ---

	b.Handle("/start", func(c telebot.Context) error {
		uid := strconv.FormatInt(c.Sender().ID, 10)
		var ni, ro string
		db.QueryRow("SELECT nickname, role FROM users WHERE tg_id=$1", uid).Scan(&ni, &ro)

		// Данные для WebApp
		rowsU, _ := db.Query("SELECT tg_id, nickname FROM users")
		var uL []UserShort
		for rowsU != nil && rowsU.Next() {
			var u UserShort; rowsU.Scan(&u.ID, &u.Nick); uL = append(uL, u)
		}
		uJ, _ := json.Marshal(uL)

		qB := `SELECT id, name, amount, rate, created_at, can_withdraw FROM bonds WHERE user_id=$1`
		rowsB, _ := db.Query(qB, uid)
		var userBonds []Bond
		for rowsB != nil && rowsB.Next() {
			var bo Bond; var t time.Time
			rowsB.Scan(&bo.ID, &bo.Name, &bo.Amount, &bo.Rate, &t, &bo.CanWithdraw)
			bo.CurrentValue = calcBond(bo.Amount, bo.Rate, t)
			bo.Date = t.Format("02.01.2006")
			userBonds = append(userBonds, bo)
		}
		bJ, _ := json.Marshal(userBonds)

		rowsM, _ := db.Query("SELECT id, name, price, rate, min_days FROM available_bonds")
		var marketBonds []MarketBond
		for rowsM != nil && rowsM.Next() {
			var mb MarketBond; rowsM.Scan(&mb.ID, &mb.Name, &mb.Price, &mb.Rate, &mb.MinDays); marketBonds = append(marketBonds, mb)
		}
		mJ, _ := json.Marshal(marketBonds)

		ba, _ := getBalance(uid)
		var inf string; db.QueryRow("SELECT text FROM info_line WHERE id=1").Scan(&inf)

		// ПЕРЕДАЕМ ПАРАМЕТРЫ НА САЙТ (включая флаг для кнопки справочника, если нужно)
		fURL := fmt.Sprintf("%s?tg_id=%s&exists=%t&nick=%s&role=%s&bal=%.2f&info=%s&users=%s&bonds=%s&market=%s",
			WebAppURL, uid, ni != "", url.QueryEscape(ni), url.QueryEscape(ro), ba, url.QueryEscape(inf), url.QueryEscape(string(uJ)), url.QueryEscape(string(bJ)), url.QueryEscape(string(mJ)))

		menu := &telebot.ReplyMarkup{ResizeKeyboard: true}
		menu.Reply(menu.Row(menu.WebApp("🇸🇪 Кабинет", &telebot.WebApp{URL: fURL})))
		
		return c.Send("🇸🇪 Система активна.", menu)
	})

	// --- ЛОГИКА WEBAPP ---
	b.Handle(telebot.OnWebApp, func(c telebot.Context) error {
		var d WebAppData
		json.Unmarshal([]byte(c.Message().WebAppData.Data), &d)
		uid := strconv.FormatInt(c.Sender().ID, 10)

		switch d.Action {
		case "buy_bond":
			var mb MarketBond
			db.QueryRow("SELECT name, price, rate FROM available_bonds WHERE id=$1", d.BondID).Scan(&mb.Name, &mb.Price, &mb.Rate)
			uBal, _ := getBalance(uid)
			if uBal < d.Amount { return c.Send("❌ Недостаточно GOLD.") }
			setBalance(uid, uBal-d.Amount)
			db.Exec("INSERT INTO bonds (user_id, name, amount, rate, created_at) VALUES ($1, $2, $3, $4, NOW())", uid, mb.Name, d.Amount, mb.Rate)
			return c.Send("✅ Куплено!")

		case "sell_bond":
			var am, rt float64; var t time.Time; var cw bool
			db.QueryRow("SELECT amount, rate, created_at, can_withdraw FROM bonds WHERE id=$1", d.BondID).Scan(&am, &rt, &t, &cw)
			if !cw { return c.Send("🔒 Заморожено.") }
			val := calcBond(am, rt, t)
			uB, _ := getBalance(uid)
			setBalance(uid, uB+val)
			db.Exec("DELETE FROM bonds WHERE id=$1", d.BondID)
			return c.Send("💰 Продано.")

		case "withdraw":
			m := &telebot.ReplyMarkup{}
			bA := m.Data("✅", "approve", fmt.Sprintf("approve:%s:%.2f", uid, d.Amount))
			bR := m.Data("❌", "reject", fmt.Sprintf("reject:%s", uid))
			m.Inline(m.Row(bA, bR))
			b.Send(&telebot.User{ID: AdminID}, fmt.Sprintf("⚠️ ЗАПРОС: %s | %.2f", d.Nick, d.Amount), m)
			return c.Send("✅ Ожидайте.")

		case "register":
			db.Exec("INSERT INTO users (tg_id, nickname, role) VALUES ($1, $2, $3)", uid, d.Nick, d.Role)
			setBalance(uid, 0)
			return c.Send("✅ Регистрация.")

		case "transfer":
			sB, _ := getBalance(uid)
			if sB < d.Amount { return c.Send("❌") }
			rB, _ := getBalance(d.TargetID)
			setBalance(uid, sB-d.Amount)
			setBalance(d.TargetID, rB+d.Amount)
			tID, _ := strconv.ParseInt(d.TargetID, 10, 64)
			b.Send(&telebot.User{ID: tID}, fmt.Sprintf("💰 +%.2f от %s", d.Amount, d.Nick))
			return c.Send("✅")
		}
		return nil
	})

	log.Println("🚀 Бот запущен!")
	b.Start()
}
