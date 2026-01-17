import os
import time
import telebot
import gspread
import psycopg2
import random
import string
from oauth2client.service_account import ServiceAccountCredentials
from flask import Flask
from threading import Thread

# --- 1. НАСТРОЙКИ ---
BOT_TOKEN = os.getenv("BOT_TOKEN")
SHEET_NAME = os.getenv("SHEET_NAME", "SwedenFINK")
GCP_JSON_DATA = os.getenv("GCP_JSON")
DATABASE_URL = os.getenv("DATABASE_URL")
ADMIN_LIST = [7631664265, 6343896085]
NOTIFY_USER_ID = 7631664265 

if GCP_JSON_DATA:
    with open("credentials.json", "w") as f:
        f.write(GCP_JSON_DATA)

# --- 2. ПОДКЛЮЧЕНИЯ ---

# Подключение к Google Таблицам
def get_google_sheet():
    try:
        scope = ["https://spreadsheets.google.com/feeds", "https://www.googleapis.com/auth/drive"]
        creds = ServiceAccountCredentials.from_json_keyfile_name("credentials.json", scope)
        gc = gspread.authorize(creds)
        return gc.open(SHEET_NAME).sheet1
    except: return None

# Подключение к PostgreSQL
def get_db_connection():
    return psycopg2.connect(DATABASE_URL)

bot = telebot.TeleBot(BOT_TOKEN)

# --- 3. ИНИЦИАЛИЗАЦИЯ И МИГРАЦИЯ ---

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

@bot.message_handler(commands=['migrate'])
def migrate_data(message):
    if message.from_user.id not in ADMIN_LIST: return

    bot.send_message(message.chat.id, "⏳ Начинаю перенос данных из Google Таблиц...")
    
    sheet = get_google_sheet()
    if not sheet:
        return bot.send_message(message.chat.id, "❌ Ошибка: не удалось подключиться к Google Таблице.")

    data = sheet.get_all_values()[1:] # Пропускаем заголовок
    conn = get_db_connection()
    
    try:
        with conn.cursor() as cur:
            for row in data:
                # Вставляем данные, если tg_id уже есть — обновляем баланс и ник
                cur.execute("""
                    INSERT INTO users (pwd, tg_id, nickname, balance, role)
                    VALUES (%s, %s, %s, %s, %s)
                    ON CONFLICT (tg_id) DO UPDATE SET
                    nickname = EXCLUDED.nickname,
                    balance = EXCLUDED.balance,
                    role = EXCLUDED.role
                """, (row[0], row[1], row[2], float(row[3].replace(',', '.')), row[4]))
        conn.commit()
        bot.send_message(message.chat.id, "✅ Данные перенесены.")
    except Exception as e:
        bot.send_message(message.chat.id, f"❌ Ошибка миграции: {e}")
    finally:
        conn.close()

# --- 4. ОСНОВНАЯ ЛОГИКА (БАЗА ДАННЫХ) ---

def get_user_db(tg_id):
    conn = get_db_connection()
    with conn.cursor() as cur:
        cur.execute("SELECT * FROM users WHERE tg_id = %s", (str(tg_id),))
        user = cur.fetchone()
    conn.close()
    return user

@bot.message_handler(commands=['start', 'profile'])
@bot.message_handler(func=lambda m: m.text == "👤 Мой профиль")
def show_profile(m):
    user = get_user_db(m.from_user.id)
    markup = telebot.types.ReplyKeyboardMarkup(resize_keyboard=True)
    
    if user:
        markup.row("👤 Мой профиль", "💸 Перевод")
        if m.from_user.id in ADMIN_LIST: markup.row("⚙️ Админ-панель")
        
        text = (f"👤 **{user[2]}**\n"
                f"💰 Баланс: **{user[3]} Gold**\n"
                f"🆔 Код: `{user[0]}`")
        
        kb = telebot.types.InlineKeyboardMarkup()
        kb.row(
            telebot.types.InlineKeyboardButton("📉 Снять Gold", callback_data="pre_withdraw"),
            telebot.types.InlineKeyboardButton("💸 Перевод", callback_data="pre_transfer")
        )
        bot.send_message(m.chat.id, text, parse_mode="Markdown", reply_markup=markup)
        bot.send_message(m.chat.id, "Действия:", reply_markup=kb)
    else:
        markup.row("📝 Регистрация")
        bot.send_message(m.chat.id, "👋 Аккаунт не найден. Зарегистрируйтесь:", reply_markup=markup)

# --- 5. ЛОГИКА ПЕРЕВОДА (БАЗА ДАННЫХ) ---

@bot.callback_query_handler(func=lambda c: c.data == "pre_transfer")
def transfer_callback(c):
    msg = bot.send_message(c.message.chat.id, "Введите часть Ника для поиска:")
    bot.register_next_step_handler(msg, search_db)

def search_db(m):
    query = f"%{m.text.lower()}%"
    conn = get_db_connection()
    with conn.cursor() as cur:
        cur.execute("SELECT nickname, tg_id FROM users WHERE LOWER(nickname) LIKE %s AND tg_id != %s LIMIT 8", (query, str(m.from_user.id)))
        found = cur.fetchall()
    conn.close()

    if not found: return bot.send_message(m.chat.id, "❌ Никто не найден.")
    
    kb = telebot.types.InlineKeyboardMarkup()
    for nick, tid in found:
        kb.add(telebot.types.InlineKeyboardButton(nick, callback_data=f"tr_{tid}"))
    bot.send_message(m.chat.id, "Выберите игрока:", reply_markup=kb)

@bot.callback_query_handler(func=lambda c: c.data.startswith("tr_"))
def ask_amt(c):
    target_id = c.data.split("_")[1]
    msg = bot.send_message(c.message.chat.id, "Сумма перевода:")
    bot.register_next_step_handler(msg, lambda m: execute_transfer(m, target_id))

def execute_transfer(m, to_id):
    try:
        amt = float(m.text)
        sender = get_user_db(m.from_user.id)
        receiver = get_user_db(to_id)

        if sender[3] < amt: return bot.send_message(m.chat.id, "❌ Недостаточно Gold.")

        conn = get_db_connection()
        with conn.cursor() as cur:
            cur.execute("UPDATE users SET balance = balance - %s WHERE tg_id = %s", (amt, str(m.from_user.id)))
            cur.execute("UPDATE users SET balance = balance + %s WHERE tg_id = %s", (amt, str(to_id)))
        conn.commit()
        conn.close()

        bot.send_message(m.chat.id, f"✅ Переведено {amt} Gold игроку {receiver[2]}.")
        bot.send_message(to_id, f"💰 Поступил перевод от {sender[2]}: +{amt} Gold")
    except: bot.send_message(m.chat.id, "❌ Ошибка.")

# --- 6. ЗАПУСК ---
app = Flask(__name__)
@app.route('/')
def h(): return "OK", 200

if __name__ == "__main__":
    init_db()
    Thread(target=lambda: app.run(host="0.0.0.0", port=8080), daemon=True).start()
    bot.remove_webhook()
    time.sleep(1)
    try: bot.send_message(NOTIFY_USER_ID, "🚀 Бот запущен (База Данных)")
    except: pass
    bot.infinity_polling(none_stop=True, skip_pending=True)
