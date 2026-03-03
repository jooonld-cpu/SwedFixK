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
const AdminID2 = 6343896085
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
	Action    string  `json:"action"`
	Nick      string  `json:"nick"`
	Role      string  `json:"role"`
	TargetID  string  `json:"target_id"`
	Amount    float64 `json:"amount"`
	BondID    int     `json:"bond_id"`
	Complaint string  `json:"complaint"`
	// Казино
	Game   string  `json:"game"`
	Bet    float64 `json:"bet"`
	Win    bool    `json:"win"`
	Payout float64 `json:"payout"`
}

var bot *telebot.Bot

func main() {
	dsn := os.Getenv("DATABASE_URL")
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal("Ошибка подключения к БД:", err)
	}
	defer db.Close()

	// ИНИЦИАЛИЗАЦИЯ ТАБЛИЦ
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS users (tg_id TEXT PRIMARY KEY, nickname TEXT, role TEXT, banned BOOLEAN DEFAULT FALSE)`); err != nil {
		log.Fatal("❌ Ошибка создания users:", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS info_line (id INT PRIMARY KEY, text TEXT)`); err != nil {
		log.Fatal("❌ Ошибка создания info_line:", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS bonds (id SERIAL PRIMARY KEY, user_id TEXT, name TEXT, amount FLOAT, rate FLOAT, created_at TIMESTAMP DEFAULT NOW(), can_withdraw BOOLEAN DEFAULT FALSE)`); err != nil {
		log.Fatal("❌ Ошибка создания bonds:", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS available_bonds (id SERIAL PRIMARY KEY, name TEXT, price FLOAT, rate FLOAT)`); err != nil {
		log.Fatal("❌ Ошибка создания available_bonds:", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS balances (user_id TEXT PRIMARY KEY, amount FLOAT DEFAULT 0)`); err != nil {
		log.Fatal("❌ Ошибка создания balances:", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS complaints (id SERIAL PRIMARY KEY, user_id TEXT, nickname TEXT, complaint TEXT, created_at TIMESTAMP DEFAULT NOW())`); err != nil {
		log.Fatal("❌ Ошибка создания complaints:", err)
	}
	db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS banned BOOLEAN DEFAULT FALSE`)

	getBalance := func(uid string) float64 {
		var a float64
		_ = db.QueryRow("SELECT COALESCE(amount, 0) FROM balances WHERE user_id=$1", uid).Scan(&a)
		return a
	}

	setBalance := func(uid string, a float64) {
		_, _ = db.Exec("INSERT INTO balances (user_id, amount) VALUES ($1, $2) ON CONFLICT (user_id) DO UPDATE SET amount=$2", uid, a)
	}

	isBanned := func(uid string) bool {
		var banned bool
		_ = db.QueryRow("SELECT COALESCE(banned, false) FROM users WHERE tg_id=$1", uid).Scan(&banned)
		return banned
	}

	isAdmin := func(id int64) bool {
		return id == AdminID || id == AdminID2
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
		http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		})

		http.HandleFunc("/api/get_user_data", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			uid := r.URL.Query().Get("uid")
			if uid == "" {
				http.Error(w, "Missing uid", http.StatusBadRequest)
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

			var lastComplaint time.Time
			db.QueryRow("SELECT COALESCE(MAX(created_at), '1970-01-01') FROM complaints WHERE user_id=$1", uid).Scan(&lastComplaint)
			canComplain := time.Since(lastComplaint).Hours() >= 12

			json.NewEncoder(w).Encode(map[string]interface{}{
				"balance":      getBalance(uid),
				"info":         info,
				"bonds":        userBonds,
				"can_complain": canComplain,
			})
		})

		http.HandleFunc("/api/get_users", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			var uL []UserShort
			rowsU, _ := db.Query("SELECT tg_id, nickname FROM users WHERE banned = false ORDER BY nickname")
			if rowsU != nil {
				defer rowsU.Close()
				for rowsU.Next() {
					var u UserShort
					rowsU.Scan(&u.ID, &u.Nick)
					uL = append(uL, u)
				}
			}
			json.NewEncoder(w).Encode(uL)
		})

		http.HandleFunc("/api/get_market", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			var mL []MarketBond
			rowsM, _ := db.Query("SELECT id, name, price, rate FROM available_bonds")
			if rowsM != nil {
				defer rowsM.Close()
				for rowsM.Next() {
					var m MarketBond
					rowsM.Scan(&m.ID, &m.Name, &m.Price, &m.Rate)
					mL = append(mL, m)
				}
			}
			json.NewEncoder(w).Encode(mL)
		})

		// ── КАЗИНО API ──────────────────────────────────────────────────
		http.HandleFunc("/api/casino", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Content-Type", "application/json")

			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var d WebAppData
			if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
				http.Error(w, "Bad request", http.StatusBadRequest)
				return
			}

			uid := r.URL.Query().Get("uid")
			if uid == "" {
				http.Error(w, "Missing uid", http.StatusBadRequest)
				return
			}

			if isBanned(uid) {
				json.NewEncoder(w).Encode(map[string]interface{}{"error": "banned"})
				return
			}

			newBal, errMsg := HandleCasino(uid, d.Game, d.Bet, d.Win, d.Payout, getBalance, setBalance)
			if errMsg != "" {
				json.NewEncoder(w).Encode(map[string]interface{}{"error": errMsg})
				return
			}

			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":      true,
				"balance": newBal,
			})
		})
		// ────────────────────────────────────────────────────────────────

		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
		log.Println("🌐 HTTP API запущен на порту:", port)
		http.ListenAndServe(":"+port, nil)
	}()

	// Самопинг чтобы Render не засыпал
	go func() {
		for {
			time.Sleep(10 * time.Minute)
			http.Get("https://swedfixk.onrender.com/health")
		}
	}()

	bot, _ = telebot.NewBot(telebot.Settings{
		Token:  os.Getenv("BOT_TOKEN"),
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	})

	// CALLBACK КНОПКИ
	bot.Handle(telebot.OnCallback, func(c telebot.Context) error {
		data := c.Callback().Data
		log.Println("📥 Получен callback:", data)

		if strings.Contains(data, "|") {
			parts := strings.Split(data, "|")
			if len(parts) > 1 {
				data = parts[1]
			}
		}

		if strings.HasPrefix(data, "approve:") {
			parts := strings.Split(data, ":")
			if len(parts) < 3 {
				c.Respond(&telebot.CallbackResponse{Text: "Ошибка данных"})
				return nil
			}
			targetID := parts[1]
			amount, _ := strconv.ParseFloat(parts[2], 64)
			cur := getBalance(targetID)
			if cur < amount {
				c.Edit("❌ ОШИБКА: Недостаточно средств у игрока.")
				c.Respond(&telebot.CallbackResponse{Text: "Мало GOLD"})
				return nil
			}
			setBalance(targetID, cur-amount)
			tID, _ := strconv.ParseInt(targetID, 10, 64)
			bot.Send(&telebot.User{ID: tID}, fmt.Sprintf("✅ Вывод одобрен!\n💰 Сумма: %.2f GOLD списано с вашего баланса.", amount))
			c.Edit(fmt.Sprintf("✅ ОДОБРЕНО\n👤 ID: %s\n💰 Сумма: %.2f GOLD", targetID, amount))
			c.Respond(&telebot.CallbackResponse{Text: "✅ Выполнено"})
			return nil
		}

		if strings.HasPrefix(data, "reject:") {
			parts := strings.Split(data, ":")
			if len(parts) < 2 {
				c.Respond(&telebot.CallbackResponse{Text: "Ошибка данных"})
				return nil
			}
			targetID := parts[1]
			tID, _ := strconv.ParseInt(targetID, 10, 64)
			bot.Send(&telebot.User{ID: tID}, "❌ Ваш запрос на вывод средств был отклонен администрацией.")
			c.Edit("❌ ОТКЛОНЕНО")
			c.Respond(&telebot.CallbackResponse{Text: "❌ Отклонено"})
			return nil
		}

		if strings.HasPrefix(data, "approve_deposit:") {
			parts := strings.Split(data, ":")
			if len(parts) < 3 {
				c.Respond(&telebot.CallbackResponse{Text: "Ошибка данных"})
				return nil
			}
			targetID := parts[1]
			amount, _ := strconv.ParseFloat(parts[2], 64)
			cur := getBalance(targetID)
			setBalance(targetID, cur+amount)
			tID, _ := strconv.ParseInt(targetID, 10, 64)
			bot.Send(&telebot.User{ID: tID}, fmt.Sprintf("✅ Пополнение подтверждено!\n💰 Сумма: %.2f GOLD зачислено на ваш баланс.", amount))
			c.Edit(fmt.Sprintf("✅ ПОПОЛНЕНИЕ ПОДТВЕРЖДЕНО\n👤 ID: %s\n💰 Сумма: %.2f GOLD", targetID, amount))
			c.Respond(&telebot.CallbackResponse{Text: "✅ Зачислено"})
			return nil
		}

		if strings.HasPrefix(data, "reject_deposit:") {
			parts := strings.Split(data, ":")
			if len(parts) < 2 {
				c.Respond(&telebot.CallbackResponse{Text: "Ошибка данных"})
				return nil
			}
			targetID := parts[1]
			tID, _ := strconv.ParseInt(targetID, 10, 64)
			bot.Send(&telebot.User{ID: tID}, "❌ Ваш запрос на пополнение был отклонен администрацией.")
			c.Edit("❌ ПОПОЛНЕНИЕ ОТКЛОНЕНО")
			c.Respond(&telebot.CallbackResponse{Text: "❌ Отклонено"})
			return nil
		}

		return nil
	})

	// АДМИН КОМАНДЫ
	bot.Handle("/set_info", func(c telebot.Context) error {
		if !isAdmin(c.Sender().ID) {
			return nil
		}
		text := strings.Join(c.Args(), " ")
		if text == "" {
			return c.Send("⚠️ Формат: /set_info [текст информации]")
		}
		_, err := db.Exec("INSERT INTO info_line (id, text) VALUES (1, $1) ON CONFLICT (id) DO UPDATE SET text = $1", text)
		if err != nil {
			return c.Send("❌ Ошибка БД: " + err.Error())
		}
		return c.Send("✅ Информационная строка обновлена!")
	})

	bot.Handle("/broadcast", func(c telebot.Context) error {
		if !isAdmin(c.Sender().ID) {
			return nil
		}
		msg := strings.Join(c.Args(), " ")
		if msg == "" {
			return c.Send("⚠️ Формат: /broadcast [сообщение для всех]")
		}
		rows, err := db.Query("SELECT tg_id FROM users WHERE banned = false")
		if err != nil {
			return c.Send("❌ Ошибка БД")
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var uid string
			rows.Scan(&uid)
			tID, _ := strconv.ParseInt(uid, 10, 64)
			if _, err := bot.Send(&telebot.User{ID: tID}, "📢 ОБЪЯВЛЕНИЕ ОТ АДМИНИСТРАЦИИ:\n\n"+msg); err == nil {
				count++
			}
			time.Sleep(50 * time.Millisecond)
		}
		return c.Send(fmt.Sprintf("✅ Рассылка завершена! Отправлено: %d пользователей", count))
	})

	bot.Handle("/ban", func(c telebot.Context) error {
		if !isAdmin(c.Sender().ID) {
			return nil
		}
		args := c.Args()
		if len(args) < 1 {
			return c.Send("⚠️ Формат: /ban [ID пользователя]")
		}
		_, err := db.Exec("UPDATE users SET banned = true WHERE tg_id = $1", args[0])
		if err != nil {
			return c.Send("❌ Ошибка БД")
		}
		tID, _ := strconv.ParseInt(args[0], 10, 64)
		bot.Send(&telebot.User{ID: tID}, "🚫 Вы были заблокированы администрацией. Доступ к системе ограничен.")
		return c.Send(fmt.Sprintf("✅ Пользователь %s заблокирован", args[0]))
	})

	bot.Handle("/unban", func(c telebot.Context) error {
		if !isAdmin(c.Sender().ID) {
			return nil
		}
		args := c.Args()
		if len(args) < 1 {
			return c.Send("⚠️ Формат: /unban [ID пользователя]")
		}
		_, err := db.Exec("UPDATE users SET banned = false WHERE tg_id = $1", args[0])
		if err != nil {
			return c.Send("❌ Ошибка БД")
		}
		tID, _ := strconv.ParseInt(args[0], 10, 64)
		bot.Send(&telebot.User{ID: tID}, "✅ Ваша блокировка снята! Доступ к системе восстановлен.")
		return c.Send(fmt.Sprintf("✅ Пользователь %s разблокирован", args[0]))
	})

	bot.Handle("/create_bond", func(c telebot.Context) error {
		if !isAdmin(c.Sender().ID) {
			return nil
		}
		args := c.Args()
		if len(args) < 3 {
			return c.Send("⚠️ Формат: /create_bond [Название] [Мин_Цена] [Процент]")
		}
		price, _ := strconv.ParseFloat(args[1], 64)
		rate, _ := strconv.ParseFloat(args[2], 64)
		_, err := db.Exec("INSERT INTO available_bonds (name, price, rate) VALUES ($1, $2, $3)", args[0], price, rate)
		if err != nil {
			return c.Send("❌ Ошибка БД")
		}
		return c.Send(fmt.Sprintf("✅ Облигация %s создана!", args[0]))
	})

	bot.Handle("/all_bonds", func(c telebot.Context) error {
		if !isAdmin(c.Sender().ID) {
			return nil
		}
		rows, err := db.Query("SELECT b.id, u.nickname, b.name, b.amount, b.rate, b.created_at, b.can_withdraw FROM bonds b JOIN users u ON b.user_id = u.tg_id ORDER BY b.id DESC")
		if err != nil {
			return c.Send("❌ Ошибка БД или данных нет.")
		}
		defer rows.Close()
		res := "📈 Все активные вклады:\n\n"
		count := 0
		for rows.Next() {
			var id int
			var nick, name string
			var am, rt float64
			var ct time.Time
			var cw bool
			if err := rows.Scan(&id, &nick, &name, &am, &rt, &ct, &cw); err == nil {
				icon := "🔒"
				if cw {
					icon = "🔓"
				}
				cur := calcBond(am, rt, ct)
				res += fmt.Sprintf("[%d] %s %s: %s\n💰 %.2f → %.2f GOLD\n📅 %s\n\n", id, icon, nick, name, am, cur, ct.Format("02.01 15:04"))
				count++
			}
		}
		if count == 0 {
			return c.Send("📈 Активных вкладов не обнаружено.")
		}
		return c.Send(res)
	})

	bot.Handle("/set_lock", func(c telebot.Context) error {
		if !isAdmin(c.Sender().ID) {
			return nil
		}
		args := c.Args()
		if len(args) < 2 {
			return c.Send("⚠️ /set_lock [ID] [1-разлок / 0-блок]")
		}
		val := args[1] == "1"
		res, err := db.Exec("UPDATE bonds SET can_withdraw = $1 WHERE id = $2", val, args[0])
		if err != nil {
			return c.Send("❌ Ошибка базы: " + err.Error())
		}
		rows, _ := res.RowsAffected()
		if rows == 0 {
			return c.Send("❌ Инвестиция с таким ID не найдена.")
		}
		status := "заблокирована"
		if val {
			status = "разблокирована"
		}
		return c.Send(fmt.Sprintf("✅ Инвестиция #%s %s.", args[0], status))
	})

	bot.Handle("/cash_all_file", func(c telebot.Context) error {
		if !isAdmin(c.Sender().ID) {
			return nil
		}
		rows, err := db.Query("SELECT u.nickname, b.amount FROM balances b JOIN users u ON b.user_id = u.tg_id")
		if err != nil || rows == nil {
			return c.Send("❌ Нечего выгружать.")
		}
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
		_ = os.WriteFile(fileName, []byte(content), 0644)
		return c.Send(&telebot.Document{File: telebot.FromDisk(fileName), FileName: fileName})
	})

	bot.Handle("/deposit", func(c telebot.Context) error {
		if !isAdmin(c.Sender().ID) {
			return nil
		}
		args := c.Args()
		if len(args) < 2 {
			return c.Send("⚠️ Формат: /deposit [ID] [Сумма]")
		}
		v, _ := strconv.ParseFloat(args[1], 64)
		setBalance(args[0], getBalance(args[0])+v)
		return c.Send(fmt.Sprintf("✅ Баланс %s пополнен на %.2f", args[0], v))
	})

	bot.Handle("/start", func(c telebot.Context) error {
		uid := strconv.FormatInt(c.Sender().ID, 10)
		if isBanned(uid) {
			return c.Send("🚫 Ваш аккаунт заблокирован. Обратитесь к администрации.")
		}
		var ni, ro string
		_ = db.QueryRow("SELECT nickname, role FROM users WHERE tg_id=$1", uid).Scan(&ni, &ro)

		uL := []UserShort{}
		rowsU, _ := db.Query("SELECT tg_id, nickname FROM users WHERE banned = false")
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

	bot.Handle(telebot.OnWebApp, func(c telebot.Context) error {
		if c.Message().WebAppData == nil {
			return nil
		}
		var d WebAppData
		if err := json.Unmarshal([]byte(c.Message().WebAppData.Data), &d); err != nil {
			return nil
		}
		uid := strconv.FormatInt(c.Sender().ID, 10)

		if isBanned(uid) {
			return c.Send("🚫 Ваш аккаунт заблокирован.")
		}

		switch d.Action {
		case "register":
			query := `INSERT INTO users (tg_id, nickname, role) VALUES ($1, $2, $3) ON CONFLICT (tg_id) DO UPDATE SET nickname = $2, role = $3`
			if _, err := db.Exec(query, uid, d.Nick, d.Role); err != nil {
				return c.Send("❌ Ошибка регистрации")
			}
			db.Exec("INSERT INTO balances (user_id, amount) VALUES ($1, 0) ON CONFLICT DO NOTHING", uid)

			uL := []UserShort{}
			rowsU, _ := db.Query("SELECT tg_id, nickname FROM users WHERE banned = false")
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
			if err := db.QueryRow("SELECT name, price, rate FROM available_bonds WHERE id=$1", d.BondID).Scan(&name, &price, &rate); err != nil {
				return c.Send("❌ Ошибка покупки: облигация не найдена.")
			}
			if getBalance(uid) < d.Amount || d.Amount < price {
				return c.Send("❌ Ошибка покупки: проверьте баланс или сумму.")
			}
			setBalance(uid, getBalance(uid)-d.Amount)
			db.Exec("INSERT INTO bonds (user_id, name, amount, rate) VALUES ($1, $2, $3, $4)", uid, name, d.Amount, rate)
			bot.Send(&telebot.User{ID: AdminID}, fmt.Sprintf("📈 НОВАЯ ИНВЕСТИЦИЯ\n👤 Игрок: %s\n💰 Сумма: %.2f GOLD\n📊 Облигация: %s\n📈 Процент: %.2f%%\n📅 Дата: %s",
				d.Nick, d.Amount, name, rate, time.Now().Format("02.01.2006 15:04")))
			return c.Send(fmt.Sprintf("✅ Вы инвестировали %.2f GOLD в %s", d.Amount, name))

		case "sell_bond":
			var am, ra float64
			var ct time.Time
			var cw bool
			if err := db.QueryRow("SELECT amount, rate, created_at, can_withdraw FROM bonds WHERE id=$1 AND user_id=$2", d.BondID, uid).Scan(&am, &ra, &ct, &cw); err != nil {
				return c.Send("❌ Инвестиция не найдена.")
			}
			if !cw {
				return c.Send("🔒 Эта инвестиция заморожена администрацией.")
			}
			val := calcBond(am, ra, ct)
			setBalance(uid, getBalance(uid)+val)
			db.Exec("DELETE FROM bonds WHERE id=$1", d.BondID)
			return c.Send(fmt.Sprintf("💰 Вклад закрыт! Получено %.2f GOLD", val))

		case "transfer":
			cur := getBalance(uid)
			if cur < d.Amount {
				return c.Send("❌ Недостаточно средств для перевода")
			}
			var senderNick string
			db.QueryRow("SELECT nickname FROM users WHERE tg_id=$1", uid).Scan(&senderNick)
			if senderNick == "" {
				senderNick = d.Nick
			}
			var receiverNick string
			db.QueryRow("SELECT nickname FROM users WHERE tg_id=$1", d.TargetID).Scan(&receiverNick)
			setBalance(uid, cur-d.Amount)
			setBalance(d.TargetID, getBalance(d.TargetID)+d.Amount)
			if targetIDInt, err := strconv.ParseInt(d.TargetID, 10, 64); err == nil {
				bot.Send(&telebot.User{ID: targetIDInt}, fmt.Sprintf("💰 Вам поступил перевод!\n👤 От: %s\n💵 Сумма: %.2f GOLD", senderNick, d.Amount))
			}
			return c.Send(fmt.Sprintf("✅ Перевод выполнен!\n👤 Получатель: %s\n💸 Сумма: %.2f GOLD", receiverNick, d.Amount))

		case "withdraw":
			markup := &telebot.ReplyMarkup{}
			btnApprove := markup.Data("✅ Одобрить", "approve", fmt.Sprintf("approve:%s:%.2f", uid, d.Amount))
			btnReject := markup.Data("❌ Отклонить", "reject", fmt.Sprintf("reject:%s", uid))
			markup.Inline(markup.Row(btnApprove, btnReject))
			bot.Send(&telebot.User{ID: AdminID}, fmt.Sprintf("⚠️ ЗАПРОС НА ВЫВОД\n👤 От: %s (ID: %s)\n💰 Сумма: %.2f GOLD", d.Nick, uid, d.Amount), markup)
			bot.Send(&telebot.User{ID: AdminID2}, fmt.Sprintf("⚠️ ЗАПРОС НА ВЫВОД\n👤 От: %s (ID: %s)\n💰 Сумма: %.2f GOLD", d.Nick, uid, d.Amount), markup)
			return c.Send("✅ Запрос на вывод отправлен администратору.")

		case "deposit_request":
			markup := &telebot.ReplyMarkup{}
			btnApprove := markup.Data("✅ Подтвердить", "approve_deposit", fmt.Sprintf("approve_deposit:%s:%.2f", uid, d.Amount))
			btnReject := markup.Data("❌ Отклонить", "reject_deposit", fmt.Sprintf("reject_deposit:%s", uid))
			markup.Inline(markup.Row(btnApprove, btnReject))
			bot.Send(&telebot.User{ID: AdminID}, fmt.Sprintf("💳 ЗАПРОС НА ПОПОЛНЕНИЕ\n👤 От: %s (ID: %s)\n💰 Сумма: %.2f GOLD\n\n⚠️ Скриншот → @Kolorli21", d.Nick, uid, d.Amount), markup)
			bot.Send(&telebot.User{ID: AdminID2}, fmt.Sprintf("💳 ЗАПРОС НА ПОПОЛНЕНИЕ\n👤 От: %s (ID: %s)\n💰 Сумма: %.2f GOLD\n\n⚠️ Скриншот → @Kolorli21", d.Nick, uid, d.Amount), markup)
			return c.Send("✅ Запрос на пополнение отправлен!\n\n📸 Не забудьте отправить скриншот в @Kolorli21!")

		case "complaint":
			var lastComplaint time.Time
			db.QueryRow("SELECT COALESCE(MAX(created_at), '1970-01-01') FROM complaints WHERE user_id=$1", uid).Scan(&lastComplaint)
			if time.Since(lastComplaint).Hours() < 12 {
				remaining := 12 - time.Since(lastComplaint).Hours()
				return c.Send(fmt.Sprintf("⏳ Следующая жалоба через %.1f часов", remaining))
			}
			if d.Complaint == "" {
				return c.Send("❌ Жалоба не может быть пустой")
			}
			db.Exec("INSERT INTO complaints (user_id, nickname, complaint) VALUES ($1, $2, $3)", uid, d.Nick, d.Complaint)
			bot.Send(&telebot.User{ID: AdminID}, fmt.Sprintf("📋 НОВАЯ ЖАЛОБА\n👤 От: %s (ID: %s)\n📅 %s\n\n💬 %s",
				d.Nick, uid, time.Now().Format("02.01.2006 15:04"), d.Complaint))
			return c.Send("✅ Жалоба отправлена администрации.")
		}
		return nil
	})

	log.Println("🚀 Бот запущен без ошибок!")
	bot.Start()
}
