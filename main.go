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
	dsn := os.Getenv("DATABASE_URL")
	if os.Getenv("DB_HOST") != "" {
		dsn = fmt.Sprintf("host=%s port=5432 user=%s password='%s' dbname=%s sslmode=require",
			os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASS"), os.Getenv("DB_NAME"))
	}
	db, err = sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal("❌ Ошибка открытия БД:", err)
	}
	defer db.Close()

	// 2. ИНИЦИАЛИЗАЦИЯ ТАБЛИЦ
	db.Exec(`CREATE TABLE IF NOT EXISTS users (tg_id TEXT PRIMARY KEY, nickname TEXT, role TEXT)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS info_line (id INT PRIMARY KEY, text TEXT)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS bonds (id SERIAL PRIMARY KEY, user_id TEXT, name TEXT, amount FLOAT, rate FLOAT, created_at TIMESTAMP DEFAULT NOW(), can_withdraw BOOLEAN DEFAULT FALSE)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS available_bonds (id SERIAL PRIMARY KEY, name TEXT, price FLOAT, rate FLOAT, min_days INT DEFAULT 0)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS balances (user_id TEXT, bank_id INT, amount FLOAT)`)

	var hasBankID bool
	db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'balances' AND column_name = 'bank_id')`).Scan(&hasBankID)

	// 3. ФУНКЦИИ ЛОГИКИ
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

	// 4. HTTP API (ДЛЯ HTML И АВТООБНОВЛЕНИЯ)
	go func() {
		http.HandleFunc("/api/get_user_data", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			uid := r.URL.Query().Get("uid")
			if uid == "" { return }

			bal, _ := getBalance(uid)
			var info string
			db.QueryRow("SELECT text FROM info_line WHERE id=1").Scan(&info)

			rowsB, _ := db.Query(`SELECT id, name, amount, rate, created_at, can_withdraw FROM bonds WHERE user_id=$1`, uid)
			var userBonds []Bond
			if rowsB != nil {
				defer rowsB.Close()
				for rowsB.Next() {
					var bo Bond; var t time.Time
					rowsB.Scan(&bo.ID, &bo.Name, &bo.Amount, &bo.Rate, &t, &bo.CanWithdraw)
					bo.CurrentValue = calcBond(bo.Amount, bo.Rate, t)
					bo.Date = t.Format("02.01.2006")
					userBonds = append(userBonds, bo)
				}
			}

			json.NewEncoder(w).Encode(map[string]interface{}{
				"balance": bal,
				"info":    info,
				"bonds":   userBonds,
			})
		})

		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "Бот Швеции активен!")
		})

		port := os.Getenv("PORT")
		if port == "" { port = "8080" }
		http.ListenAndServe(":"+port, nil)
	}()

	// 5. НАСТРОЙКА ТЕЛЕГРАМ БОТА
	b, err := telebot.NewBot(telebot.Settings{
		Token:  os.Getenv("BOT_TOKEN"),
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil { log.Fatal(err) }

	// ОБРАБОТКА КНОПОК ОДОБРЕНИЯ
	b.Handle(telebot.OnCallback, func(c telebot.Context) error {
		data := c.Callback().Data
		if strings.Contains(data, "approve") {
			parts := strings.Split(data, ":")
			targetID := parts[1]
			amount, _ := strconv.ParseFloat(parts[2], 64)
			cur, _ := getBalance(targetID)
			setBalance(targetID, cur-amount)
			tID, _ := strconv.ParseInt(targetID, 10, 64)
			b.Send(&telebot.User{ID: tID}, fmt.Sprintf("✅ Вывод одобрен: %.2f GOLD.", amount))
			c.Edit(fmt.Sprintf("✅ ОДОБРЕНО\nСумма: %.2f", amount))
		}
		if strings.Contains(data, "reject") {
			c.Edit("❌ ОТКЛОНЕНО")
		}
		return c.Respond()
	})

	// АДМИН КОМАНДЫ
	b.Handle("/all_bonds", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		rows, _ := db.Query(`SELECT b.id, u.nickname, b.name, b.amount, b.can_withdraw FROM bonds b JOIN users u ON b.user_id = u.tg_id`)
		defer rows.Close()
		var res strings.Builder
		res.WriteString("📜 **РЕЕСТР:**\n")
		for rows.Next() {
			var bid int; var nick, bname string; var am float64; var cw bool
			rows.Scan(&bid, &nick, &bname, &am, &cw)
			lock := "🔒"; if cw { lock = "🔓" }
			res.WriteString(fmt.Sprintf("%d | %s | %s | %.2f %s\n", bid, nick, bname, am, lock))
		}
		return c.Send(res.String())
	})

	b.Handle("/set_lock", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		args := c.Args()
		if len(args) < 2 { return c.Send("⚠️ /set_lock [ID] [1-разлок / 0-блок]") }
		val := args[1] == "1"
		db.Exec("UPDATE bonds SET can_withdraw = $1 WHERE id = $2", val, args[0])
		return c.Send("✅ Статус изменен.")
	})

	b.Handle("/create_bond", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		a := c.Args()
		if len(a) < 3 { return c.Send("⚠️ /create_bond [Имя] [Цена] [%]") }
		p, _ := strconv.ParseFloat(a[1], 64)
		r, _ := strconv.ParseFloat(a[2], 64)
		db.Exec("INSERT INTO available_bonds (name, price, rate) VALUES ($1, $2, $3)", a[0], p, r)
		return c.Send("✅ Облигация добавлена.")
	})

	b.Handle("/deposit", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		a := c.Args()
		if len(a) < 2 { return c.Send("⚠️ /deposit [ID] [Сумма]") }
		val, _ := strconv.ParseFloat(a[1], 64)
		cur, _ := getBalance(a[0])
		setBalance(a[0], cur+val)
		return c.Send("✅ Баланс пополнен.")
	})

	// СТАРТ И WEBAPP
	b.Handle("/start", func(c telebot.Context) error {
		uid := strconv.FormatInt(c.Sender().ID, 10)
		var ni, ro string
		db.QueryRow("SELECT nickname, role FROM users WHERE tg_id=$1", uid).Scan(&ni, &ro)

		// Сбор всех данных для WebApp (JSON)
		rowsU, _ := db.Query("SELECT tg_id, nickname FROM users")
		var uL []UserShort
		for rowsU.Next() {
			var u UserShort; rowsU.Scan(&u.ID, &u.Nick); uL = append(uL, u)
		}
		rowsU.Close()
		uJ, _ := json.Marshal(uL)

		rowsM, _ := db.Query("SELECT id, name, price, rate, min_days FROM available_bonds")
		var mL []MarketBond
		for rowsM.Next() {
			var m MarketBond; rowsM.Scan(&m.ID, &m.Name, &m.Price, &m.Rate, &m.MinDays); mL = append(mL, m)
		}
		rowsM.Close()
		mJ, _ := json.Marshal(mL)

		bal, _ := getBalance(uid)
		var inf string; db.QueryRow("SELECT text FROM info_line WHERE id=1").Scan(&inf)

		fURL := fmt.Sprintf("%s?tg_id=%s&exists=%t&nick=%s&role=%s&bal=%.2f&info=%s&users=%s&market=%s",
			WebAppURL, uid, ni != "", url.QueryEscape(ni), url.QueryEscape(ro), bal, 
			url.QueryEscape(inf), url.QueryEscape(string(uJ)), url.QueryEscape(string(mJ)))

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
			setBalance(uid, 0)
			return c.Send("✅ Регистрация завершена!")

		case "buy_bond":
			var mb MarketBond
			db.QueryRow("SELECT name, price, rate FROM available_bonds WHERE id=$1", d.BondID).Scan(&mb.Name, &mb.Price, &mb.Rate)
			cur, _ := getBalance(uid)
			if cur < d.Amount { return c.Send("❌ Мало GOLD") }
			setBalance(uid, cur-d.Amount)
			db.Exec("INSERT INTO bonds (user_id, name, amount, rate, created_at, can_withdraw) VALUES ($1, $2, $3, $4, NOW(), FALSE)", uid, mb.Name, d.Amount, mb.Rate)
			return c.Send("✅ Инвестиция открыта!")

		case "sell_bond":
			var am, rt float64; var t time.Time; var cw bool
			db.QueryRow("SELECT amount, rate, created_at, can_withdraw FROM bonds WHERE id=$1", d.BondID).Scan(&am, &rt, &t, &cw)
			if !cw { return c.Send("🔒 Вклад заморожен.") }
			val := calcBond(am, rt, t)
			cur, _ := getBalance(uid)
			setBalance(uid, cur+val)
			db.Exec("DELETE FROM bonds WHERE id=$1", d.BondID)
			return c.Send(fmt.Sprintf("💰 Получено %.2f GOLD.", val))

		case "transfer":
			sB, _ := getBalance(uid)
			if sB < d.Amount { return c.Send("❌ Мало GOLD") }
			rB, _ := getBalance(d.TargetID)
			setBalance(uid, sB-d.Amount)
			setBalance(d.TargetID, rB+d.Amount)
			return c.Send("✅ Перевод выполнен.")

		case "withdraw":
			m := &telebot.ReplyMarkup{}
			bA := m.Data("✅", "approve", fmt.Sprintf("approve:%s:%.2f", uid, d.Amount))
			bR := m.Data("❌", "reject", fmt.Sprintf("reject:%s", uid))
			m.Inline(m.Row(bA, bR))
			b.Send(&telebot.User{ID: AdminID}, fmt.Sprintf("⚠️ ЗАПРОС: %s | %.2f GOLD", d.Nick, d.Amount), m)
			return c.Send("✅ Запрос отправлен.")
		}
		return nil
	})

	log.Println("🚀 Бот запущен!")
	b.Start()
}
