package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/lib/pq"
	"gopkg.in/telebot.v3"
)

const AdminID = 7631664265
const WebAppURL = "https://jooonld-cpu.github.io/SwedenFixKFront.github.io/"

type WebAppData struct {
	Action string  `json:"action"`
	Nick   string  `json:"nick"`
	Role   string  `json:"role"`
	Target string  `json:"target_id"`
	Type   string  `json:"type"`
	Amount float64 `json:"amount"`
}

func main() {
	// Подключение к БД
	db, err := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}

	// Инициализация таблицы
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users (
		tg_id TEXT PRIMARY KEY,
		nickname TEXT,
		balance FLOAT DEFAULT 0,
		role TEXT
	)`)

	// Настройка бота
	pref := telebot.Settings{
		Token:  os.Getenv("BOT_TOKEN"),
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	}

	b, err := telebot.NewBot(pref)
	if err != nil {
		log.Fatal(err)
	}

	// Запуск Health Check сервера для Koyeb (чтобы не было ошибки 8080)
	go http.ListenAndServe(":8080", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "OK")
	}))

	// Обработка /start
	b.Handle("/start", func(c telebot.Context) error {
		uid := strconv.FormatInt(c.Sender().ID, 10)
		var nick, role string
		var balance float64

		err := db.QueryRow("SELECT nickname, balance, role FROM users WHERE tg_id=$1", uid).Scan(&nick, &balance, &role)
		
		exists := "true"
		if err == sql.ErrNoRows {
			exists = "false"
		}

		isAdmin := "false"
		if c.Sender().ID == AdminID {
			isAdmin = "true"
		}

		// Формируем URL для Mini App
		finalURL := fmt.Sprintf("%s?exists=%s&admin=%s&nick=%s&role=%s&balance=%f&v=%d",
			WebAppURL, exists, isAdmin, nick, role, balance, time.Now().Unix())

		menu := &telebot.ReplyMarkup{ResizeKeyboard: true}
		btn := menu.WebApp("💎 Открыть Меню", &telebot.WebApp{URL: finalURL})
		menu.Reply(menu.Row(btn))

		return c.Send("Нажмите кнопку ниже, чтобы войти в кабинет или зарегистрироваться:", menu)
	})

	// Обработка данных из Web App
	b.Handle(telebot.OnWebApp, func(c telebot.Context) error {
		var data WebAppData
		err := json.Unmarshal([]byte(c.Message().WebAppData.Data), &data)
		if err != nil {
			return nil
		}

		if data.Action == "register" {
			uid := strconv.FormatInt(c.Sender().ID, 10)
			_, err := db.Exec("INSERT INTO users (tg_id, nickname, role, balance) VALUES ($1, $2, $3, 0) ON CONFLICT (tg_id) DO UPDATE SET nickname=$2, role=$3",
				uid, data.Nick, data.Role)
			if err != nil {
				return c.Send("Ошибка сохранения.")
			}
			return c.Send(fmt.Sprintf("✅ Регистрация завершена!\nНик: %s\nДолжность: %s\n\nНажми /start ещё раз.", data.Nick, data.Role))
		}
		return nil
	})

	// АДМИН КОМАНДЫ (текстовые)
	b.Handle("/set", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		args := c.Args() // /set ID Сумма
		if len(args) < 2 { return c.Send("Используй: /set ID Сумма") }
		db.Exec("UPDATE users SET balance = balance + $1 WHERE tg_id = $2", args[1], args[0])
		return c.Send("✅ Баланс изменен.")
	})

	b.Handle("/del", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		db.Exec("DELETE FROM users WHERE tg_id = $1", c.Args()[0])
		return c.Send("❌ Пользователь удален.")
	})

	b.Handle("/reset", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		db.Exec("UPDATE users SET balance = 0 WHERE tg_id = $1", c.Args()[0])
		return c.Send("🧹 Баланс обнулен.")
	})package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/lib/pq"
	"gopkg.in/telebot.v3"
)

const AdminID = 7631664265
const WebAppURL = "https://jooonld-cpu.github.io/SwedenFixKFront.github.io/"

type WebAppData struct {
	Action string  `json:"action"`
	Nick   string  `json:"nick"`
	Role   string  `json:"role"`
	Target string  `json:"target_id"`
	Type   string  `json:"type"`
	Amount float64 `json:"amount"`
}

func main() {
	db, err := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users (
		tg_id TEXT PRIMARY KEY,
		nickname TEXT,
		balance FLOAT DEFAULT 0,
		role TEXT
	)`)

	pref := telebot.Settings{
		Token:  os.Getenv("BOT_TOKEN"),
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	}

	b, err := telebot.NewBot(pref)
	if err != nil {
		log.Fatal(err)
	}

	go http.ListenAndServe(":8080", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "OK")
	}))

	// --- ВСПОМОГАТЕЛЬНАЯ ФУНКЦИЯ ДЛЯ СБОРА ДАННЫХ ---
	generateBackup := func() (string, error) {
		rows, err := db.Query("SELECT tg_id, nickname, balance, role FROM users")
		if err != nil {
			return "", err
		}
		defer rows.Close()

		filePath := "db_backup.txt"
		file, err := os.Create(filePath)
		if err != nil {
			return "", err
		}
		defer file.Close()

		fmt.Fprintf(file, "Бэкап БД от %s\n", time.Now().Format("2006-01-02 15:04:05"))
		fmt.Fprintln(file, "--------------------------------------------------")

		for rows.Next() {
			var tid, nick, role string
			var bal float64
			if err := rows.Scan(&tid, &nick, &bal, &role); err == nil {
				fmt.Fprintf(file, "ID: %s | Ник: %s | Баланс: %.2f | Роль: %s\n", tid, nick, bal, role)
			}
		}
		return filePath, nil
	}

	// --- КОМАНДА /cash_all (Только экспорт) ---
	b.Handle("/cash_all", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }

		path, err := generateBackup()
		if err != nil {
			return c.Send("Ошибка генерации файла: " + err.Error())
		}
		defer os.Remove(path)

		doc := &telebot.Document{File: telebot.FromDisk(path), FileName: "Full_DB_Backup.txt"}
		return c.Send(doc)
	})

	// --- КОМАНДА /clear_all (Экспорт + Удаление с подтверждением) ---
	confirmMenu := &telebot.ReplyMarkup{}
	btnConfirm := confirmMenu.Data("⚠️ ПОДТВЕРДИТЬ УДАЛЕНИЕ", "confirm_delete_all")

	b.Handle("/clear_all", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		confirmMenu.Inline(confirmMenu.Row(btnConfirm))
		return c.Send("Вы собираетесь полностью очистить базу данных. Бэкап будет создан автоматически. Продолжить?", confirmMenu)
	})

	b.Handle(&btnConfirm, func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }

		// 1. Делаем бэкап
		path, err := generateBackup()
		if err == nil {
			doc := &telebot.Document{File: telebot.FromDisk(path), FileName: "Backup_Before_Wipe.txt"}
			c.Send("Копия перед удалением:", doc)
			os.Remove(path)
		}

		// 2. Очищаем таблицу
		_, err = db.Exec("DELETE FROM users")
		if err != nil {
			return c.Edit("Ошибка при удалении: " + err.Error())
		}

		return c.Edit("✅ База данных успешно очищена. Все данные удалены.")
	})

	// --- ОСТАЛЬНЫЕ ОБРАБОТЧИКИ ---
	b.Handle("/start", func(c telebot.Context) error {
		uid := strconv.FormatInt(c.Sender().ID, 10)
		var nick, role string
		var balance float64

		err := db.QueryRow("SELECT nickname, balance, role FROM users WHERE tg_id=$1", uid).Scan(&nick, &balance, &role)
		
		exists := "true"
		if err == sql.ErrNoRows { exists = "false" }

		isAdmin := "false"
		if c.Sender().ID == AdminID { isAdmin = "true" }

		finalURL := fmt.Sprintf("%s?exists=%s&admin=%s&nick=%s&role=%s&balance=%f&v=%d",
			WebAppURL, exists, isAdmin, nick, role, balance, time.Now().Unix())

		menu := &telebot.ReplyMarkup{ResizeKeyboard: true}
		btn := menu.WebApp("💎 Открыть Меню", &telebot.WebApp{URL: finalURL})
		menu.Reply(menu.Row(btn))

		return c.Send("Нажмите кнопку ниже, чтобы войти в кабинет или зарегистрироваться:", menu)
	})

	b.Handle(telebot.OnWebApp, func(c telebot.Context) error {
		var data WebAppData
		err := json.Unmarshal([]byte(c.Message().WebAppData.Data), &data)
		if err != nil { return nil }

		if data.Action == "register" {
			uid := strconv.FormatInt(c.Sender().ID, 10)
			_, err := db.Exec("INSERT INTO users (tg_id, nickname, role, balance) VALUES ($1, $2, $3, 0) ON CONFLICT (tg_id) DO UPDATE SET nickname=$2, role=$3",
				uid, data.Nick, data.Role)
			if err != nil { return c.Send("Ошибка сохранения.") }
			return c.Send(fmt.Sprintf("✅ Регистрация завершена!\nНик: %s\nДолжность: %s\n\nНажми /start ещё раз.", data.Nick, data.Role))
		}
		return nil
	})

	b.Handle("/set", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		args := c.Args()
		if len(args) < 2 { return c.Send("Используй: /set ID Сумма") }
		db.Exec("UPDATE users SET balance = balance + $1 WHERE tg_id = $2", args[1], args[0])
		return c.Send("✅ Баланс изменен.")
	})

	b.Handle("/del", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		if len(c.Args()) < 1 { return c.Send("Укажите ID") }
		db.Exec("DELETE FROM users WHERE tg_id = $1", c.Args()[0])
		return c.Send("❌ Пользователь удален.")
	})

	b.Handle("/reset", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		if len(c.Args()) < 1 { return c.Send("Укажите ID") }
		db.Exec("UPDATE users SET balance = 0 WHERE tg_id = $1", c.Args()[0])
		return c.Send("🧹 Баланс обнулен.")
	})

	b.Send(&telebot.User{ID: AdminID}, "🚀 Бот на Go запущен и готов к работе!")
	log.Println("Бот запущен...")
	b.Start()
}
