package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"gopkg.in/telebot.v3"
)

// Конфигурация
const AdminID = 7631664265
const WebAppURL = "https://jooonld-cpu.github.io/SwedenFixKFront.github.io/"

type WebAppData struct {
	Action string  `json:"action"`
	Nick   string  `json:"nick"`
	Role   string  `json:"role"`
	Target int64   `json:"target_id"`
	Type   string  `json:"type"`
	Amount float64 `json:"amount"`
}

func main() {
	// 1. Подключение к БД
	db, err := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal("Ошибка БД:", err)
	}
	defer db.Close()

	// Создание таблицы если нет
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users (
		tg_id TEXT PRIMARY KEY,
		nickname TEXT,
		balance FLOAT DEFAULT 0,
		role TEXT
	)`)

	// 2. Настройка бота
	pref := telebot.Settings{
		Token:  os.Getenv("BOT_TOKEN"),
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	}

	b, err := telebot.NewBot(pref)
	if err != nil {
		log.Fatal(err)
	}

	// Уведомление о запуске
	b.Send(&telebot.User{ID: AdminID}, "🚀 Бот успешно запущен на Golang!")

	// 3. Обработка команды /start
	b.Handle("/start", func(c telebot.Context) error {
		var exists bool
		var nick, role string
		var balance float64
		uid := strconv.FormatInt(c.Sender().ID, 10)

		err := db.QueryRow("SELECT nickname, balance, role FROM users WHERE tg_id=$1", uid).Scan(&nick, &balance, &role)
		if err == sql.ErrNoRows {
			exists = false
		} else {
			exists = true
		}

		isAdmin := c.Sender().ID == AdminID
		// Формируем ссылку для Web App
		url := fmt.Sprintf("%s?exists=%t&admin=%t&nick=%s&role=%s&balance=%f&v=%d", 
			WebAppURL, exists, isAdmin, nick, role, balance, time.Now().Unix())

		menu := &telebot.ReplyMarkup{ResizeKeyboard: true}
		btn := menu.WebApp("💎 Открыть Меню", &telebot.WebApp{URL: url})
		menu.Reply(menu.Row(btn))

		text := "👋 Добро пожаловать! Используйте меню ниже:"
		if !exists {
			text = "👋 Привет! Нужно зарегистрироваться через меню."
		}
		return c.Send(text, menu)
	})

	// 4. Обработка данных из Mini App (Регистрация)
	b.Handle(telebot.OnWebApp, func(c telebot.Context) error {
		var data WebAppData
		err := json.Unmarshal([]byte(c.Message().WebAppData.Data), &data)
		if err != nil {
			return c.Send("Ошибка данных")
		}

		if data.Action == "register" {
			uid := strconv.FormatInt(c.Sender().ID, 10)
			_, err := db.Exec("INSERT INTO users (tg_id, nickname, balance, role) VALUES ($1, $2, 0, $3)", 
				uid, data.Nick, data.Role)
			if err != nil {
				return c.Send("Ошибка регистрации в БД")
			}
			return c.Send(fmt.Sprintf("✅ Готово, %s! Нажми /start снова.", data.Nick))
		}
		return nil
	})

	// 5. Админ-команды через текст (Пример: /manage ID действие сумма)
	// Формат: /set 123456789 100 (добавить 100)
	b.Handle("/set", func(c telebot.Context) error {
		if c.Sender().ID != AdminID {
			return nil
		}
		args := c.Args()
		if len(args) < 2 {
			return c.Send("Используй: /set [ID] [Сумма]")
		}
		targetID := args[0]
		amount, _ := strconv.ParseFloat(args[1], 64)

		_, err := db.Exec("UPDATE users SET balance = balance + $1 WHERE tg_id = $2", amount, targetID)
		if err != nil {
			return c.Send("Ошибка БД")
		}
		b.Send(&telebot.User{ID: AdminID}, "✅ Баланс обновлен")
		// Уведомляем пользователя
		tid, _ := strconv.ParseInt(targetID, 10, 64)
		b.Send(&telebot.User{ID: tid}, fmt.Sprintf("💰 Ваш баланс изменен на %f", amount))
		return nil
	})

	// Команда удаления
	b.Handle("/del", func(c telebot.Context) error {
		if c.Sender().ID != AdminID { return nil }
		targetID := c.Args()[0]
		db.Exec("DELETE FROM users WHERE tg_id = $1", targetID)
		return c.Send("❌ Профиль удален")
	})

	log.Println("Бот в эфире...")
	b.Start()
}
