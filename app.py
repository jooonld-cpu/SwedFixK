import os
import time
import json
import telebot
import gspread
import psycopg2
import random
import string
from oauth2client.service_account import ServiceAccountCredentials
from flask import Flask
from threading import Thread

# --- 1. НАСТРОЙКИ ---
# [cite_start]Используйте ваш НОВЫЙ токен, чтобы избежать ошибки 409 [cite: 5, 39, 54]
BOT_TOKEN = os.getenv("BOT_TOKEN")
DATABASE_URL = os.getenv("DATABASE_URL")
# Ссылка на ваш GitHub Pages
WEB_APP_URL = "https://jooonld-cpu.github.io/SwedenFixKFront.github.io/"
ADMIN_ID = 7631664265 

# Настройки для миграции из Google Таблиц
SHEET_NAME = os.getenv("SHEET_NAME", "SwedenFINK")
GCP_JSON_DATA = os.getenv("GCP_JSON")

if GCP_JSON_DATA:
    with open("credentials.json", "w") as f:
        f.write(GCP_JSON_DATA)

bot = telebot.TeleBot(BOT_TOKEN)

# --- 2. РАБОТА С БАЗОЙ ДАННЫХ (PostgreSQL) ---

def get_db_connection():
    return psycopg2.connect(DATABASE_URL)

def init_db():
    conn = get_db_connection()
    with conn.cursor() as cur:
        cur.execute("""
            CREATE TABLE IF NOT EXISTS users (
                pwd TEXT,
                tg_id TEXT PRIMARY KEY,
                nickname TEXT,
                balance FLOAT DEFAULT 0,
                role TEXT DEFAULT 'Игрок'
            )
        """)
    conn.commit()
    conn.close()

# --- 3. АВТОМАТИЧЕСКАЯ МИГРАЦИЯ ---

def run_auto_migration():
    try:
        scope = ["https://spreadsheets.google.com/feeds", "https://www.googleapis.com/auth/drive"]
        creds = ServiceAccountCredentials.from_json_keyfile_name("credentials.json", scope)
        gc = gspread.authorize(creds)
        sheet = gc.open(SHEET_NAME).sheet1
        
        data = sheet.get_all_values()[1:]
        conn = get_db_connection()
        with conn.cursor() as cur:
            for row in data:
                cur.execute("""
                    INSERT INTO users (pwd, tg_id, nickname, balance, role)
                    VALUES (%s, %s, %s, %s, %s)
                    ON CONFLICT (tg_id) DO UPDATE SET
                    nickname = EXCLUDED.nickname,
                    balance = EXCLUDED.balance
                """, (row[0], row[1], row[2], float(row[3].replace(',', '.')), row[4]))
        conn.commit()
        conn.close()
        print("✅ Миграция завершена")
    except Exception as e:
        print(f"⚠️ Миграция пропущена или ошибка: {e}")

# --- 4. ОБРАБОТЧИКИ КОМАНД ---

@bot.message_handler(commands=['start'])
def welcome(m):
    uid = str(m.from_user.id)
    conn = get_db_connection()
    with conn.cursor() as cur:
        cur.execute("SELECT * FROM users WHERE tg_id = %s", (uid,))
        user = cur.fetchone()
    conn.close()

    if not user:
        msg = bot.send_message(m.chat.id, "👋 Привет! Твой аккаунт не найден. Введи свой Ник для регистрации:")
        bot.register_next_step_handler(msg, process_registration)
    else:
        # Показываем кнопку открытия сайта (Web App)
        markup = telebot.types.ReplyKeyboardMarkup(resize_keyboard=True)
        web_info = telebot.types.WebAppInfo(WEB_APP_URL)
        markup.add(telebot.types.KeyboardButton("💎 Личный кабинет", web_app=web_info))
        
        bot.send_message(m.chat.id, f"С возвращением, {user[2]}!", reply_markup=markup)

def process_registration(m):
    pwd = ''.join(random.choices(string.ascii_letters + string.digits, k=12))
    conn = get_db_connection()
    with conn.cursor() as cur:
        cur.execute("INSERT INTO users (pwd, tg_id, nickname, balance) VALUES (%s, %s, %s, %s)",
                    (pwd, str(m.from_user.id), m.text, 0.0))
    conn.commit()
    conn.close()
    bot.send_message(m.chat.id, "✅ Регистрация завершена! Нажми /start")

# --- 5. ОБРАБОТКА ДАННЫХ ИЗ WEB APP ---

@bot.message_handler(content_types=['web_app_data'])
def handle_web_app_data(m):
    # Получаем JSON, который отправил ваш GitHub Pages
    data = json.loads(m.web_app_data.data)
    
    if data.get('action') == 'withdraw':
        amount = data.get('amount')
        
        # Кнопки для админа
        kb = telebot.types.InlineKeyboardMarkup()
        kb.add(telebot.types.InlineKeyboardButton("✅ Одобрить", callback_data=f"adm_ok_{m.from_user.id}_{amount}"))
        
        bot.send_message(ADMIN_ID, f"🚨 Заявка на снятие!\nИгрок: {m.from_user.first_name}\nСумма: {amount} Gold", reply_markup=kb)
        bot.send_message(m.chat.id, f"⌛ Запрос на снятие {amount} Gold отправлен администрации.")

@bot.callback_query_handler(func=lambda c: c.data.startswith("adm_ok_"))
def approve_withdraw(c):
    _, _, uid, amt = c.data.split("_")
    conn = get_db_connection()
    with conn.cursor() as cur:
        cur.execute("UPDATE users SET balance = balance - %s WHERE tg_id = %s", (float(amt), uid))
    conn.commit()
    conn.close()
    
    bot.edit_message_text(f"✅ Выплачено {amt} Gold пользователю {uid}", c.message.chat.id, c.message.message_id)
    bot.send_message(uid, f"✅ Твой вывод на {amt} Gold одобрен!")

# --- 6. ЗАПУСК ---
app = Flask(__name__)
@app.route('/')
def health(): return "OK", 200

if __name__ == "__main__":
    # [cite_start]Запуск сервера Flask для Koyeb [cite: 36, 43]
    Thread(target=lambda: app.run(host="0.0.0.0", port=8080), daemon=True).start()
    
    init_db()
    run_auto_migration()
    
    # [cite_start]Решение ошибки 409 Conflict [cite: 5, 8, 32, 39]
    bot.remove_webhook()
    time.sleep(2)
    
    print("🚀 Бот запущен (Web App Mode)")
    bot.infinity_polling(none_stop=True, skip_pending=True)

