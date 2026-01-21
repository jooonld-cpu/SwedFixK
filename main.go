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
	"time"

	_ "github.com/lib/pq"
	"gopkg.in/telebot.v3"
)

const AdminID = 7631664265
const WebAppURL = "https://jooonld-cpu.github.io/SwedenFixKFront.github.io/"

type MarketBond struct {
	ID    int     `json:"id"`
	Name  string  `json:"name"`
	Price float64 `json:"price"`
	Rate  float64 `json:"rate"`
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
	if err != nil {
		log.Fatal("Ошибка подключения к БД:", err)
	}
	defer db.Close()

	// --- ИНИЦИАЛИЗАЦИЯ ТАБЛИЦ (ТВОИ ЛОГИ СОХРАНЕНЫ) ---
	
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS users (tg_id TEXT PRIMARY KEY, nickname TEXT, role TEXT)`); err != nil {
		log.Fatal("❌ КРИТИЧЕСКАЯ ОШИБКА: Не удалось создать таблицу users: ", err)
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS info_line (id INT PRIMARY KEY, text TEXT)`); err != nil {
		log.Fatal("❌ Ошибка создания info_line: ", err)
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS bonds (id SERIAL PRIMARY KEY, user_id TEXT, name TEXT, amount FLOAT, rate FLOAT, created_at TIMESTAMP DEFAULT NOW(), can_withdraw BOOLEAN DEFAULT FALSE)`); err != nil {
		log.Fatal("❌ Ошибка создания bonds: ", err)
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS available_bonds (id SERIAL PRIMARY KEY, name TEXT, price FLOAT, rate FLOAT)`); err != nil {
		log.Fatal("❌ Ошибка создания available_bonds: ", err)
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS balances (user_id TEXT PRIMARY KEY, amount FLOAT DEFAULT 0)`); err != nil {
		log.Fatal("❌ Ошибка создания balances: ", err)
	}

	getBalance := func(uid string) float64 {
		var a float64
		_ = db.QueryRow("SELECT COALESCE(amount, 0) FROM balances WHERE user_id=$1", uid).Scan(&a)
		return a
	}

	setBalance := func(uid string, a float64) {
		_, _ = db.Exec("INSERT INTO balances (user_id, amount) VALUES ($1, $2) ON CONFLICT (user_id) DO UPDATE SET amount=$2", uid, a)
	}

	calcBond := func(amount, rate float64, t time.Time) float64 {
		days := math.Floor(time.Since(t).Hours() / 24)
		if days <= 0 {
			return amount
		}
		return amount * math.Pow(1+(rate/100), days)
	}

	// HTTP API
	go func() {
		http.HandleFunc("/api/get_user_data", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			uid := r.URL.Query().Get("uid")
			if uid == "" {
				return
			}

			var info string
			_ = db.QueryRow("SELECT text FROM info_line WHERE id=1").Scan(&info)

			rows, err := db.Query("SELECT id, name, amount, rate, created_at, can_withdraw FROM bonds WHERE user_id=$1", uid)
			var userBonds []Bond
			if err == nil && rows != nil {
				defer rows.Close()
				for rows.Next() {
					var b Bond
					var ct time.Time
					if err := rows.Scan(&b.ID, &b.Name, &b.Amount, &b.Rate, &ct, &b.CanWithdraw); err == nil {
						b.CurrentValue = calcBond(b.Amount, b.Rate, ct)
						b.Date = ct.Format("02.01.2006")
						userBonds = append(userBonds, b)
					}
				}
			}

			json.NewEncoder(w).Encode(map[string]interface{}{
				"balance": getBalance(uid),
				"info":    info,
				"bonds":   userBonds,
			})
		})
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
		http.ListenAndServe(":"+port, nil)
	}()

	b, _ := telebot.NewBot(telebot.Settings{
		Token:  os.Getenv("BOT_TOKEN"),
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	})

	// --- ВСЕ АДМИН КОМАНДЫ ВОССТАНОВЛЕНЫ ---

	b.Handle("/create_bond", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		args := c.Args()
		if len(args) < 3 { return c.Send("⚠️ Формат: /create_bond [Название] [Мин_Цена] [Процент]") }
		name := args[0]
		price, _ := strconv.ParseFloat(args[1], 64)
		rate, _ := strconv.ParseFloat(args[2], 64)
		_, err := db.Exec("INSERT INTO available_bonds (name, price, rate) VALUES ($1, $2, $3)", name, price, rate)
		if err != nil { return c.Send("❌ Ошибка БД") }
		return c.Send(fmt.Sprintf("✅ Облигация %s создана!", name))
	})

	b.Handle("/all_bonds", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		rows, err := db.Query("SELECT b.id, u.nickname, b.name, b.amount FROM bonds b JOIN users u ON b.user_id = u.tg_id")
		if err != nil { return c.Send("❌ Ошибка БД или данных нет.") }
		if rows == nil { return c.Send("📈 Вкладов пока нет.") }
		defer rows.Close()

		res := "📈 Все активные вклады:\n"
		count := 0
		for rows.Next() {
			var id int
			var nick, name string
			var am float64
			if err := rows.Scan(&id, &nick, &name, &am); err == nil {
				res += fmt.Sprintf("[%d] %s: %s (%.2f)\n", id, nick, name, am)
				count++
			}
		}
		if count == 0 { return c.Send("📈 Активных вкладов не обнаружено.") }
		return c.Send(res)
	})

	b.Handle("/cash_all_file", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		rows, err := db.Query("SELECT u.nickname, b.amount FROM balances b JOIN users u ON b.user_id = u.tg_id")
		if err != nil || rows == nil { return c.Send("❌ Нечего выгружать.") }
		defer rows.Close()

		content := "--- РЕЕСТР БАЛАНСОВ ---\n"
		for rows.Next() {
			var n string
			var a float64
			if err := rows.Scan(&n, &a); err == nil {
				content += fmt.Sprintf("%s: %.2f GOLD\n", n, a)
			}
		}

		fileName := "balances.txt"
		os.WriteFile(fileName, []byte(content), 0644)
		return c.Send(&telebot.Document{File: telebot.FromDisk(fileName), FileName: fileName})
	})

	b.Handle("/deposit", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		args := c.Args()
		if len(args) < 2 { return c.Send("⚠️ Формат: /deposit [ID] [Сумма]") }
		v, _ := strconv.ParseFloat(args[1], 64)
		setBalance(args[0], getBalance(args[0])+v)
		return c.Send(fmt.Sprintf("✅ Баланс %s пополнен на %.2f", args[0], v))
	})

	b.Handle("/start", func(c telebot.Context) error {
		uid := strconv.FormatInt(c.Sender().ID, 10)
		var ni, ro string
		_ = db.QueryRow("SELECT nickname, role FROM users WHERE tg_id=$1", uid).Scan(&ni, &ro)

		uL := []UserShort{}
		rowsU, _ := db.Query("SELECT tg_id, nickname FROM users")
		if rowsU != nil {
			defer rowsU.Close()
			for rowsU.Next() {
				var u UserShort
				rowsU.Scan(&u.ID, &u.Nick)
				uL = append(uL, u)
			}
		}
		uJ, _ := json.Marshal(uL)

		mL := []MarketBond{}
		rowsM, _ := db.Query("SELECT id, name, price, rate FROM available_bonds")
		if rowsM != nil {
			defer rowsM.Close()
			for rowsM.Next() {
				var m MarketBond
				rowsM.Scan(&m.ID, &m.Name, &m.Price, &m.Rate)
				mL = append(mL, m)
			}
		}
		mJ, _ := json.Marshal(mL)

		fURL := fmt.Sprintf("%s?tg_id=%s&exists=%t&nick=%s&role=%s&bal=%.2f&users=%s&market=%s",
			WebAppURL, uid, ni != "", url.QueryEscape(ni), url.QueryEscape(ro), getBalance(uid),
			url.QueryEscape(string(uJ)), url.QueryEscape(string(mJ)))

		menu := &telebot.ReplyMarkup{ResizeKeyboard: true}
		menu.Reply(menu.Row(menu.WebApp("🇸🇪 Открыть банк", &telebot.WebApp{URL: fURL})))
		return c.Send("🇸🇪 Добро пожаловать в финансовую систему Швеции.", menu)
	})

	// --- ОБРАБОТКА ДАННЫХ ИЗ WEBAPP (С МГНОВЕННЫМ ВХОДОМ И БАЛАНСОМ) ---

	b.Handle(telebot.OnWebApp, func(c telebot.Context) error {
		if c.Message().WebAppData == nil { return nil }
		var d WebAppData
		err := json.Unmarshal([]byte(c.Message().WebAppData.Data), &d)
		if err != nil { return nil }
		uid := strconv.FormatInt(c.Sender().ID, 10)

		switch d.Action {
		case "register":
			query := `INSERT INTO users (tg_id, nickname, role) VALUES ($1, $2, $3) ON CONFLICT (tg_id) DO UPDATE SET nickname = $2, role = $3`
			_, err := db.Exec(query, uid, d.Nick, d.Role)
			if err != nil { return c.Send("❌ Ошибка регистрации") }
			
			// Инициализация баланса БЕЗ ОБНУЛЕНИЯ существующих денег
			db.Exec("INSERT INTO balances (user_id, amount) VALUES ($1, 0) ON CONFLICT DO NOTHING", uid)

			// --- МГНОВЕННЫЙ ВХОД (ГЕНЕРАЦИЯ НОВОГО МЕНЮ) ---
			uL := []UserShort{}
			rowsU, _ := db.Query("SELECT tg_id, nickname FROM users")
			if rowsU != nil {
				defer rowsU.Close()
				for rowsU.Next() {
					var u UserShort
					rowsU.Scan(&u.ID, &u.Nick)
					uL = append(uL, u)
				}
			}
			uJ, _ := json.Marshal(uL)

			mL := []MarketBond{}
			rowsM, _ := db.Query("SELECT id, name, price, rate FROM available_bonds")
			if rowsM != nil {
				defer rowsM.Close()
				for rowsM.Next() {
					var m MarketBond
					rowsM.Scan(&m.ID, &m.Name, &m.Price, &m.Rate)
					mL = append(mL, m)
				}
			}
			mJ, _ := json.Marshal(mL)

			fURL := fmt.Sprintf("%s?tg_id=%s&exists=true&nick=%s&role=%s&bal=%.2f&users=%s&market=%s",
				WebAppURL, uid, url.QueryEscape(d.Nick), url.QueryEscape(d.Role), getBalance(uid),
				url.QueryEscape(string(uJ)), url.QueryEscape(string(mJ)))

			menu := &telebot.ReplyMarkup{ResizeKeyboard: true}
			menu.Reply(menu.Row(menu.WebApp("🇸🇪 Открыть банк", &telebot.WebApp{URL: fURL})))
			return c.Send("✅ Регистрация завершена! Аккаунт активирован:", menu)

		case "buy_bond":
			var price, rate float64
			var name string
			err := db.QueryRow("SELECT name, price, rate FROM available_bonds WHERE id=$1", d.BondID).Scan(&name, &price, &rate)
			if err != nil || getBalance(uid) < d.Amount || d.Amount < price {
				return c.Send("❌ Ошибка покупки: проверьте баланс или сумму.")
			}
			setBalance(uid, getBalance(uid)-d.Amount)
			db.Exec("INSERT INTO bonds (user_id, name, amount, rate) VALUES ($1, $2, $3, $4)", uid, name, d.Amount, rate)
			return c.Send(fmt.Sprintf("✅ Вы инвестировали %.2f GOLD в %s", d.Amount, name))

		case "sell_bond":
			var am, ra float64
			var ct time.Time
			err := db.QueryRow("SELECT amount, rate, created_at FROM bonds WHERE id=$1 AND user_id=$2", d.BondID, uid).Scan(&am, &ra, &ct)
			if err != nil { return c.Send("❌ Инвестиция не найдена.") }
			val := calcBond(am, ra, ct)
			setBalance(uid, getBalance(uid)+val)
			db.Exec("DELETE FROM bonds WHERE id=$1", d.BondID)
			return c.Send(fmt.Sprintf("💰 Вклад закрыт! Получено %.2f GOLD", val))

		case "transfer":
			cur := getBalance(uid)
			if cur < d.Amount { return c.Send("❌ Недостаточно средств") }
			setBalance(uid, cur-d.Amount)
			setBalance(d.TargetID, getBalance(d.TargetID)+d.Amount)
			return c.Send(fmt.Sprintf("💸 Перевод %.2f GOLD выполнен успешно!", d.Amount))

		case "withdraw":
			b.Send(&telebot.User{ID: AdminID}, fmt.Sprintf("⚠️ ЗАПРОС НА ВЫВОД\nОт: %s\nСумма: %.2f GOLD\nID: %s", d.Nick, d.Amount, uid))
			return c.Send("✅ Ваш запрос на вывод средств отправлен на проверку.")
		}
		return nil
	})

	log.Println("🚀 Бот запущен без ошибок!")
	b.Start()
}
