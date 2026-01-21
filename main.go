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
	// ИСПРАВЛЕНИЕ ПОДКЛЮЧЕНИЯ:
	// Мы проверяем наличие отдельных переменных для БД (хост, пароль и т.д.).
	// Это самый надежный способ избежать ошибок со спецсимволами вроде [ ] + / в пароле
	// и принудительно использовать IPv4 через redwood.dev для обхода ошибки network unreachable.
	var db *sql.DB
	var err error

	dbHost := os.Getenv("DB_HOST")
	if dbHost != "" {
		// Собираем DSN формат. Одинарные кавычки вокруг пароля позволяют использовать любые символы.
		dsn := fmt.Sprintf("host=%s port=5432 user=%s password='%s' dbname=%s sslmode=require",
			dbHost, os.Getenv("DB_USER"), os.Getenv("DB_PASS"), os.Getenv("DB_NAME"))
		db, err = sql.Open("postgres", dsn)
	} else {
		// Если переменных нет, пытаемся обработать DATABASE_URL
		rawURL := os.Getenv("DATABASE_URL")
		if strings.HasPrefix(rawURL, "postgres://") || strings.HasPrefix(rawURL, "postgresql://") {
			u, parseErr := url.Parse(rawURL)
			if parseErr == nil {
				pass, _ := u.User.Password()
				host := u.Host
				user := u.User.Username()
				dbname := strings.TrimPrefix(u.Path, "/")
				// Пересобираем в безопасный DSN формат
				dsn := fmt.Sprintf("host=%s user=%s password='%s' dbname=%s sslmode=require", 
					host, user, pass, dbname)
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

	// Проверка соединения с БД
	if err := db.Ping(); err != nil {
		log.Println("❌ БД недоступна (проверьте параметры):", err)
	}

	// 1. ИНИЦИАЛИЗАЦИЯ ТАБЛИЦ
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
	log.Printf("✅ Инициализация: bank_id=%v", hasBankID)

	// 2. ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ
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

	// 3. HTTP БЛОК ДЛЯ RENDER + API
	go func() {
		http.HandleFunc("/api/get_user_data", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			uid := r.URL.Query().Get("uid")
			if uid == "" { return }
			bal, _ := getBalance(uid)
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
			json.NewEncoder(w).Encode(map[string]interface{}{"balance": bal, "bonds": userBonds})
		})
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "Бот Швеции активен!")
		})
		port := os.Getenv("PORT")
		if port == "" { port = "8080" }
		http.ListenAndServe(":"+port, nil)
	}()

	// 4. НАСТРОЙКА БОТА
	b, err := telebot.NewBot(telebot.Settings{
		Token:  os.Getenv("BOT_TOKEN"),
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil { log.Fatal(err) }

	// CALLBACK-И
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

	// АДМИН КОМАНДЫ
	b.Handle("/set_info", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		
		txt := strings.TrimSpace(c.Message().Payload)
		if txt == "" {
			if len(c.Args()) > 0 {
				txt = strings.Join(c.Args(), " ")
			}
		}

		if txt == "" {
			return c.Send("⚠️ Напишите текст: /set_info Новости дня...")
		}
		_, err := db.Exec("INSERT INTO info_line (id, text) VALUES (1, $1) ON CONFLICT (id) DO UPDATE SET text = $1", txt)
		if err != nil {
			return c.Send("❌ Ошибка БД: " + err.Error())
		}
		return c.Send("✅ Инфо-линия обновлена!")
	})

	b.Handle("/create_bond", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		a := c.Args()
		if len(a) < 4 { return c.Send("⚠️ /create_bond [Имя] [Цена] [Процент] [Дни]") }
		p, _ := strconv.ParseFloat(a[1], 64)
		r, _ := strconv.ParseFloat(a[2], 64)
		d, _ := strconv.Atoi(a[3])
		db.Exec("INSERT INTO available_bonds (name, price, rate, min_days) VALUES ($1, $2, $3, $4)", a[0], p, r, d)
		return c.Send("✅ Товар добавлен в магазин.")
	})

	b.Handle("/del_bond", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		if len(c.Args()) < 1 { return c.Send("ID товара?") }
		db.Exec("DELETE FROM available_bonds WHERE id = $1", c.Args()[0])
		return c.Send("🗑 Товар удален.")
	})

	b.Handle("/set_lock", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		a := c.Args()
		if len(a) < 2 { return c.Send("⚠️ /set_lock [ID_ВКЛАДА] [1/0]") }
		val := a[1] == "1"
		db.Exec("UPDATE bonds SET can_withdraw = $1 WHERE id = $2", val, a[0])
		return c.Send("✅ Статус вклада изменен.")
	})

	b.Handle("/all_bonds", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		rows, err := db.Query("SELECT b.id, u.nickname, b.name, b.amount, b.can_withdraw FROM bonds b JOIN users u ON b.user_id = u.tg_id")
		if err != nil { return c.Send("❌ Ошибка БД") }
		defer rows.Close()
		var res strings.Builder
		res.WriteString("📈 **ВКЛАДЫ:**\n\n")
		for rows.Next() {
			var id int; var n, bn string; var am float64; var cw bool
			rows.Scan(&id, &n, &bn, &am, &cw)
			st := "🔒"; if cw { st = "🔓" }
			res.WriteString(fmt.Sprintf("%d | %s | %s | %.2f | %s\n", id, n, bn, am, st))
		}
		return c.Send(res.String(), telebot.ModeMarkdown)
	})

	b.Handle("/cash_all", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		rows, err := db.Query("SELECT u.nickname, COALESCE(b.amount, 0) FROM users u LEFT JOIN balances b ON b.user_id = u.tg_id")
		if err != nil { return c.Send("❌ Ошибка БД") }
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
		rows, _ := db.Query("SELECT u.tg_id, u.nickname, COALESCE(b.amount, 0) FROM users u LEFT JOIN balances b ON u.tg_id = b.user_id")
		defer rows.Close()
		txt := "ID | NICK | BALANCE\n"
		for rows.Next() {
			var id, ni string; var a float64
			rows.Scan(&id, &ni, &a); txt += fmt.Sprintf("%s | %s | %.2f\n", id, ni, a)
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
		return c.Send("✅ Пользователь добавлен.")
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
		if len(a) < 2 { return c.Send("ID СУММА?") }
		v, _ := strconv.ParseFloat(a[1], 64)
		cur, _ := getBalance(a[0])
		setBalance(a[0], cur+v)
		return c.Send("✅ Пополнено.")
	})

	// /START
	b.Handle("/start", func(c telebot.Context) error {
		uid := strconv.FormatInt(c.Sender().ID, 10)
		var ni, ro string
		db.QueryRow("SELECT nickname, role FROM users WHERE tg_id=$1", uid).Scan(&ni, &ro)
		rowsU, _ := db.Query("SELECT tg_id, nickname FROM users ORDER BY nickname")
		var uL []UserShort
		if rowsU != nil {
			defer rowsU.Close()
			for rowsU.Next() {
				var u UserShort; rowsU.Scan(&u.ID, &u.Nick); uL = append(uL, u)
			}
		}
		uJ, _ := json.Marshal(uL)
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
		bJ, _ := json.Marshal(userBonds)
		rowsM, _ := db.Query("SELECT id, name, price, rate, min_days FROM available_bonds")
		var marketBonds []MarketBond
		if rowsM != nil {
			defer rowsM.Close()
			for rowsM.Next() {
				var mb MarketBond; rowsM.Scan(&mb.ID, &mb.Name, &mb.Price, &mb.Rate, &mb.MinDays); marketBonds = append(marketBonds, mb)
			}
		}
		mJ, _ := json.Marshal(marketBonds)
		ba, _ := getBalance(uid)
		var inf string; db.QueryRow("SELECT text FROM info_line WHERE id=1").Scan(&inf)

		fURL := fmt.Sprintf("%s?tg_id=%s&exists=%t&nick=%s&role=%s&bal=%.2f&info=%s&users=%s&bonds=%s&market=%s",
			WebAppURL, uid, ni != "", url.QueryEscape(ni), url.QueryEscape(ro), ba, url.QueryEscape(inf), url.QueryEscape(string(uJ)), url.QueryEscape(string(bJ)), url.QueryEscape(string(mJ)))

		menu := &telebot.ReplyMarkup{ResizeKeyboard: true}
		menu.Reply(menu.Row(menu.WebApp("🇸🇪 Кабинет", &telebot.WebApp{URL: fURL})))
		return c.Send("🇸🇪 Система активна.", menu)
	})

	// WEBAPP LOGIC
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
		case "sell_bond":
			var am, rt float64; var t time.Time; var cw bool
			db.QueryRow("SELECT amount, rate, created_at, can_withdraw FROM bonds WHERE id=$1", d.BondID).Scan(&am, &rt, &t, &cw)
			if !cw { return c.Send("🔒 Заморожено.") }
			val := calcBond(am, rt, t)
			uB, _ := getBalance(uid); setBalance(uid, uB+val)
			db.Exec("DELETE FROM bonds WHERE id=$1", d.BondID)
			return c.Send("💰 Продано.")
		case "withdraw":
			m := &telebot.ReplyMarkup{}
			bA := m.Data("✅", "approve", fmt.Sprintf("approve:%s:%.2f", uid, d.Amount))
			bR := m.Data("❌", "reject", fmt.Sprintf("reject:%s", uid))
			m.Inline(m.Row(bA, bR))
			b.Send(&telebot.User{ID: AdminID}, fmt.Sprintf("⚠️ ЗАПРОС: %s | %.2f", d.Nick, d.Amount), m)
			return c.Send("✅ Запрос отправлен.")
		case "register":
			db.Exec("INSERT INTO users (tg_id, nickname, role) VALUES ($1, $2, $3)", uid, d.Nick, d.Role)
			setBalance(uid, 0)
			return c.Send("✅ Регистрация.")
		case "transfer":
			sB, _ := getBalance(uid)
			if sB < d.Amount { return c.Send("❌") }
			rB, _ := getBalance(d.TargetID)
			setBalance(uid, sB-d.Amount); setBalance(d.TargetID, rB+d.Amount)
			tID, _ := strconv.ParseInt(d.TargetID, 10, 64)
			b.Send(&telebot.User{ID: tID}, fmt.Sprintf("💰 +%.2f GOLD от %s", d.Amount, d.Nick))
			return c.Send("✅ Перевод выполнен.")
		}
		return nil
	})

	log.Println("🚀 Бот запущен!")
	b.Start()
}
