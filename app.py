import os, time, json, telebot, psycopg2, random, string
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
    cur.execute("SELECT balance, nickname, role FROM users WHERE tg_id = %s", (uid,))
    user = cur.fetchone()
    conn.close()

    is_admin = "true" if m.from_user.id == ADMIN_ID else "false"
    
    if not user:
        # Пользователь не найден -> отправляем на регистрацию на сайте
        link = f"{WEB_APP_URL}?exists=false&admin={is_admin}&v={time.time()}"
        text = "👋 Добро пожаловать! Для начала работы нужно зарегистрироваться через наше меню:"
    else:
        # Пользователь найден -> отправляем личные данные
        link = f"{WEB_APP_URL}?exists=true&balance={user[0]}&nick={user[1]}&role={user[2]}&admin={is_admin}&v={time.time()}"
        text = f"🛡️ Личный кабинет игрока {user[1]}"

    markup = telebot.types.ReplyKeyboardMarkup(resize_keyboard=True)
    markup.add(telebot.types.KeyboardButton("💎 Открыть Меню", web_app=telebot.types.WebAppInfo(link)))
    bot.send_message(m.chat.id, text, reply_markup=markup)

@bot.message_handler(content_types=['web_app_data'])
def handle_web_app_data(m):
    data = json.loads(m.web_app_data.data)
    uid = str(m.from_user.id)

    # 1. РЕГИСТРАЦИЯ
    if data.get('action') == 'register':
        conn = get_db(); cur = conn.cursor()
        cur.execute("INSERT INTO users (tg_id, nickname, balance, role) VALUES (%s, %s, 0, %s) ON CONFLICT DO NOTHING",
                    (uid, data['nick'], data['role']))
        conn.commit(); conn.close()
        bot.send_message(m.chat.id, f"✅ Регистрация успешна! Ник: {data['nick']}, Должность: {data['role']}. Нажми /start ещё раз.")

    # 2. АДМИН-УПРАВЛЕНИЕ
    elif data.get('action') == 'admin_manage' and m.from_user.id == ADMIN_ID:
        tid, t_type, amt = data['target_id'], data['type'], data['amount']
        conn = get_db(); cur = conn.cursor()
        
        if t_type == 'add':
            cur.execute("UPDATE users SET balance = balance + %s WHERE tg_id = %s", (amt, tid))
        elif t_type == 'sub':
            cur.execute("UPDATE users SET balance = balance - %s WHERE tg_id = %s", (amt, tid))
        elif t_type == 'reset':
            cur.execute("UPDATE users SET balance = 0 WHERE tg_id = %s", (tid,))
        
        conn.commit(); conn.close()
        bot.send_message(m.chat.id, "✅ Изменения внесены!")
        bot.send_message(tid, f"📢 Ваш баланс был обновлен администратором.")

    # 3. СПИСОК ЮЗЕРОВ (для админа)
    elif data.get('action') == 'get_users_list' and m.from_user.id == ADMIN_ID:
        conn = get_db(); cur = conn.cursor()
        cur.execute("SELECT nickname, balance, role, tg_id FROM users")
        users = cur.fetchall()
        conn.close()
        
        msg = "📋 **УПРАВЛЕНИЕ ИГРОКАМИ:**\n\n"
        for u in users:
            msg += f"👤 {u[0]} ({u[2]})\n💰 Баланс: {u[1]}G | ID: `{u[3]}`\n"
            msg += f"Действия: /add_{u[3]} | /sub_{u[3]} | /reset_{u[3]}\n\n"
        bot.send_message(m.chat.id, msg, parse_mode="Markdown")

# --- Запуск Flask и Бота ---
app = Flask(__name__)
@app.route('/')
def h(): return "OK", 200

if __name__ == "__main__":
    Thread(target=lambda: app.run(host="0.0.0.0", port=8080), daemon=True).start()
    bot.remove_webhook()
    time.sleep(2)
    try: bot.send_message(ADMIN_ID, "🚀 Бот и Mini App запущены!")
    except: pass
    bot.infinity_polling(none_stop=True, skip_pending=True)
