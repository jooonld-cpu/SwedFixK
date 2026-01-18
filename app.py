import os
import time
import json
import telebot
import psycopg2
import random
import string
from flask import Flask
from threading import Thread

# --- КОНФИГ ---
BOT_TOKEN = os.getenv("BOT_TOKEN")
DATABASE_URL = os.getenv("DATABASE_URL")
WEB_APP_URL = "https://jooonld-cpu.github.io/SwedenFixKFront.github.io/"
ADMIN_ID = 7631664265

bot = telebot.TeleBot(BOT_TOKEN)

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

# --- ЛОГИКА БОТА ---

@bot.message_handler(commands=['start'])
def start_cmd(m):
    uid = str(m.from_user.id)
    conn = get_db_connection()
    cur = conn.cursor()
    cur.execute("SELECT balance, nickname FROM users WHERE tg_id = %s", (uid,))
    user = cur.fetchone()
    conn.close()

    if not user:
        msg = bot.send_message(m.chat.id, "👋 Привет! Ты не зарегистрирован. Введи свой Ник:")
        bot.register_next_step_handler(msg, register_user)
    else:
        balance = user[0]
        # Важно: добавляем баланс в URL как параметр
        app_url_with_data = f"{WEB_APP_URL}?balance={balance}"
        
        markup = telebot.types.ReplyKeyboardMarkup(resize_keyboard=True)
        web_info = telebot.types.WebAppInfo(app_url_with_data)
        markup.add(telebot.types.KeyboardButton("💎 Личный кабинет", web_app=web_info))
        
        bot.send_message(m.chat.id, f"Добро пожаловать, {user[1]}!", reply_markup=markup)

def register_user(m):
    pwd = ''.join(random.choices(string.ascii_letters + string.digits, k=12))
    conn = get_db_connection()
    cur = conn.cursor()
    cur.execute("INSERT INTO users (pwd, tg_id, nickname, balance) VALUES (%s, %s, %s, %s)",
                (pwd, str(m.from_user.id), m.text, 0.0))
    conn.commit()
    conn.close()
    bot.send_message(m.chat.id, "✅ Регистрация завершена! Нажми /start")

# Обработка данных из красивого окна
@bot.message_handler(content_types=['web_app_data'])
def handle_app_data(m):
    data = json.loads(m.web_app_data.data)
    if data.get('action') == 'withdraw':
        amount = data.get('amount')
        
        # Проверка баланса перед заявкой
        conn = get_db_connection()
        cur = conn.cursor()
        cur.execute("SELECT balance FROM users WHERE tg_id = %s", (str(m.from_user.id),))
        current_balance = cur.fetchone()[0]
        conn.close()

        if current_balance < amount:
            bot.send_message(m.chat.id, f"❌ Ошибка! Недостаточно средств (Баланс: {current_balance})")
            return

        kb = telebot.types.InlineKeyboardMarkup()
        kb.add(telebot.types.InlineKeyboardButton("✅ Одобрить", callback_data=f"payout_{m.from_user.id}_{amount}"))
        
        bot.send_message(ADMIN_ID, f"🚨 **ЗАЯВКА НА ВЫВОД**\nИгрок: {m.from_user.first_name}\nСумма: {amount} Gold", reply_markup=kb)
        bot.send_message(m.chat.id, "⌛ Ваша заявка отправлена администратору.")

@bot.callback_query_handler(func=lambda c: c.data.startswith("payout_"))
def admin_payout(c):
    _, uid, amt = c.data.split("_")
    conn = get_db_connection()
    cur = conn.cursor()
    cur.execute("UPDATE users SET balance = balance - %s WHERE tg_id = %s", (float(amt), uid))
    conn.commit()
    conn.close()
    
    bot.edit_message_text(f"✅ Выплата {amt} Gold подтверждена!", c.message.chat.id, c.message.message_id)
    bot.send_message(uid, f"🎁 Твой вывод на {amt} Gold успешно выполнен!")

# --- СЕРВЕР ---
app = Flask(__name__)
@app.route('/')
def health(): return "OK", 200

if __name__ == "__main__":
    init_db()
    Thread(target=lambda: app.run(host="0.0.0.0", port=8080), daemon=True).start()
    bot.remove_webhook()
    time.sleep(2)
    bot.infinity_polling(none_stop=True, skip_pending=True)

