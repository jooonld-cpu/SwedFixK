import os, time, json, telebot, psycopg2, random, string
from flask import Flask
from threading import Thread

BOT_TOKEN = os.getenv("BOT_TOKEN")
DATABASE_URL = os.getenv("DATABASE_URL")
WEB_APP_URL = "https://jooonld-cpu.github.io/SwedenFixKFront.github.io/"
ADMIN_ID = 7631664265 # Ваш ID

bot = telebot.TeleBot(BOT_TOKEN)

def get_db(): return psycopg2.connect(DATABASE_URL)

@bot.message_handler(commands=['start'])
def start(m):
    uid = str(m.from_user.id)
    conn = get_db(); cur = conn.cursor()
    cur.execute("SELECT balance, nickname, role FROM users WHERE tg_id = %s", (uid,))
    user = cur.fetchone()
    conn.close()

    if not user:
        bot.send_message(m.chat.id, "Зарегистрируйтесь, введя ник:")
        bot.register_next_step_handler(m, register)
    else:
        is_admin = "true" if m.from_user.id == ADMIN_ID else "false"
        # Передаем статус админа в URL
        app_url = f"{WEB_APP_URL}?balance={user[0]}&admin={is_admin}"
        
        markup = telebot.types.ReplyKeyboardMarkup(resize_keyboard=True)
        markup.add(telebot.types.KeyboardButton("💎 Кабинет", web_app=telebot.types.WebAppInfo(app_url)))
        bot.send_message(m.chat.id, f"Привет, {user[1]}!", reply_markup=markup)

def register(m):
    conn = get_db(); cur = conn.cursor()
    cur.execute("INSERT INTO users (tg_id, nickname, balance) VALUES (%s, %s, 0) ON CONFLICT DO NOTHING", (str(m.from_user.id), m.text))
    conn.commit(); conn.close()
    bot.send_message(m.chat.id, "Готово! Жми /start")

@bot.message_handler(content_types=['web_app_data'])
def handle_data(m):
    data = json.loads(m.web_app_data.data)
    
    # Логика для админа: Пополнение/Снятие через сайт
    if data.get('action') == 'admin_manage' and m.from_user.id == ADMIN_ID:
        tid, amt, t_type = data['target_id'], data['amount'], data['type']
        op = "+" if t_type == 'add' else "-"
        conn = get_db(); cur = conn.cursor()
        cur.execute(f"UPDATE users SET balance = balance {op} %s WHERE tg_id = %s", (amt, tid))
        conn.commit(); conn.close()
        bot.send_message(m.chat.id, f"✅ Баланс игрока {tid} изменен на {amt}")
        bot.send_message(tid, f"💰 Ваш баланс изменен на {op}{amt} Gold администратором.")

    # Логика запроса списка юзеров для админа
    elif data.get('action') == 'get_users_list' and m.from_user.id == ADMIN_ID:
        conn = get_db(); cur = conn.cursor()
        cur.execute("SELECT nickname, balance, role, tg_id FROM users LIMIT 20")
        users = cur.fetchall()
        conn.close()
        res = "👥 Список игроков:\n" + "\n".join([f"• {u[0]} | {u[1]}G | {u[2]} (ID:{u[3]})" for u in users])
        bot.send_message(m.chat.id, res)

    # Обычное снятие
    elif data.get('action') == 'withdraw':
        bot.send_message(ADMIN_ID, f"🚨 Заявка на вывод: {m.from_user.first_name} ({data['amount']} Gold)")
        bot.send_message(m.chat.id, "Заявка отправлена!")

app = Flask(__name__)
@app.route('/')
def h(): return "OK", 200

if __name__ == "__main__":
    Thread(target=lambda: app.run(host="0.0.0.0", port=8080), daemon=True).start()
    
    # 1. ЗАЩИТА ОТ 409
    bot.remove_webhook()
    time.sleep(2)
    
    # 2. УВЕДОМЛЕНИЕ О ЗАПУСКЕ
    try:
        bot.send_message(ADMIN_ID, "🚀 **Бот успешно перезапущен!**\nВсе системы работают через PostgreSQL.")
    except: pass

    bot.infinity_polling(none_stop=True, skip_pending=True)
