import os, time, json, telebot, psycopg2, random
from flask import Flask
from threading import Thread

BOT_TOKEN = os.getenv("BOT_TOKEN")
DATABASE_URL = os.getenv("DATABASE_URL")
WEB_APP_URL = "https://jooonld-cpu.github.io/SwedenFixKFront.github.io/"
ADMIN_ID = 7631664265 

bot = telebot.TeleBot(BOT_TOKEN)

def get_db(): return psycopg2.connect(DATABASE_URL)

@bot.message_handler(commands=['start'])
def start(m):
    uid = str(m.from_user.id)
    conn = get_db(); cur = conn.cursor()
    cur.execute("SELECT balance, nickname FROM users WHERE tg_id = %s", (uid,))
    user = cur.fetchone()
    conn.close()

    if not user:
        bot.send_message(m.chat.id, "Зарегистрируйтесь: введите ник.")
        bot.register_next_step_handler(m, register)
    else:
        # ПРОВЕРКА
        is_admin_flag = "false"
        if m.from_user.id == ADMIN_ID:
            is_admin_flag = "true"
            bot.send_message(m.chat.id, "🛡️ Система распознала вас как АДМИНА.")

        # Ссылка с балансом и флагом админа
        link = f"{WEB_APP_URL}?balance={user[0]}&admin={is_admin_flag}&t={int(time.time())}"
        
        markup = telebot.types.ReplyKeyboardMarkup(resize_keyboard=True)
        markup.add(telebot.types.KeyboardButton("💎 Кабинет", web_app=telebot.types.WebAppInfo(link)))
        bot.send_message(m.chat.id, f"Привет, {user[1]}!", reply_markup=markup)

def register(m):
    conn = get_db(); cur = conn.cursor()
    cur.execute("INSERT INTO users (tg_id, nickname, balance) VALUES (%s, %s, 0) ON CONFLICT DO NOTHING", (str(m.from_user.id), m.text))
    conn.commit(); conn.close()
    bot.send_message(m.chat.id, "Готово! Жми /start")

@bot.message_handler(content_types=['web_app_data'])
def handle_app_data(m):
    data = json.loads(m.web_app_data.data)
    
    if data.get('action') == 'get_users_list' and m.from_user.id == ADMIN_ID:
        conn = get_db(); cur = conn.cursor()
        cur.execute("SELECT nickname, balance, tg_id FROM users LIMIT 15")
        rows = cur.fetchall()
        conn.close()
        text = "👥 ИГРОКИ:\n" + "\n".join([f"• {r[0]}: {r[1]}G (ID:{r[2]})" for r in rows])
        bot.send_message(m.chat.id, text)

    elif data.get('action') == 'withdraw':
        bot.send_message(ADMIN_ID, f"🚨 Вывод: {m.from_user.first_name} - {data['amount']}G")
        bot.send_message(m.chat.id, "Заявка принята!")

app = Flask(__name__)
@app.route('/')
def h(): return "OK", 200

if __name__ == "__main__":
    Thread(target=lambda: app.run(host="0.0.0.0", port=8080), daemon=True).start()
    bot.remove_webhook()
    time.sleep(2)
    try: bot.send_message(ADMIN_ID, "🚀 Запущен!")
    except: pass
    bot.infinity_polling(none_stop=True, skip_pending=True)
